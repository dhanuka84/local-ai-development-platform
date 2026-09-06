package service

import (
	"context"
	"errors"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
)

type governanceFake struct {
	*fakeRepository
	validation    domain.KnowledgeValidation
	decision      domain.KnowledgeDecision
	decisionCalls int
}

func (f *governanceFake) RecordKnowledgeValidation(_ context.Context, v domain.KnowledgeValidation) (domain.KnowledgeValidation, error) {
	f.validation = v
	return v, nil
}
func (f *governanceFake) GetKnowledgeValidation(context.Context, string) (domain.KnowledgeValidation, error) {
	return f.validation, nil
}
func (f *governanceFake) DecideKnowledge(_ context.Context, d domain.KnowledgeDecision) (domain.KnowledgeItem, error) {
	f.decision = d
	f.decisionCalls++
	return f.items[0], nil
}

func TestValidationBindsAuthenticatedHumanAndVerifiesSourceBytes(t *testing.T) {
	store := artifacts.NewLocalStore(t.TempDir())
	ctx := identity.WithPrincipal(context.Background(), soloDeveloper())
	artifact, err := store.Put(ctx, []byte("source fixture"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-4000-8000-000000000001"
	repo := &governanceFake{fakeRepository: &fakeRepository{items: []domain.KnowledgeItem{{ID: id, ProjectID: "product", Version: 2, Content: "verified procedure", Status: domain.CandidatePending}}}}
	svc := New(repo, store, nil, nil, false, false)
	if err := svc.ConfigureAuthorization(&fakeAuthorizer{decision: domain.AuthorizationDecision{Allowed: true}}, false); err != nil {
		t.Fatal(err)
	}
	input := ValidationInput{KnowledgeID: id, ExpectedVersion: 2, SourceManifest: domain.SourceManifest{SchemaVersion: "hybrid-ai/knowledge-source/v1", Sources: []domain.KnowledgeSource{{Kind: "procedure", Reference: "fixture", ArtifactSHA256: artifact.SHA256}}}, Criteria: []domain.ValidationCriterion{{Name: "reproduced", Passed: true, Observation: "Reproduced with fixture"}}}
	report, err := svc.RecordManualValidation(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if report.ValidatedBy != soloDeveloper().ID || report.CandidateVersion != 2 || report.ContentSHA256 != domain.Digest([]byte("verified procedure")) || report.Verdict != "pass" {
		t.Fatalf("incorrect report: %#v", report)
	}
	if _, err := store.Read(ctx, report.ReportArtifact.SHA256); err != nil {
		t.Fatal(err)
	}
	input.ExpectedVersion = 1
	if _, err := svc.RecordManualValidation(ctx, input); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale version: %v", err)
	}
	input.ExpectedVersion = 2
	input.SourceManifest.Sources[0].ArtifactSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := svc.RecordManualValidation(ctx, input); !errors.Is(err, domain.ErrEvidenceUnavailable) {
		t.Fatalf("missing artifact: %v", err)
	}
	input.SourceManifest.Sources[0].ArtifactSHA256 = artifact.SHA256
	workload := soloDeveloper()
	workload.Human = false
	if _, err := svc.RecordManualValidation(identity.WithPrincipal(context.Background(), workload), input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("workload validation: %v", err)
	}
}

func TestDecisionRequiresEvidenceAndDerivesActor(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	repo := &governanceFake{fakeRepository: &fakeRepository{items: []domain.KnowledgeItem{{ID: id, ProjectID: "product", Version: 1, Content: "fixture", Status: domain.CandidatePending}}}}
	store := artifacts.NewLocalStore(t.TempDir())
	svc := New(repo, store, nil, nil, false, false)
	if err := svc.ConfigureAuthorization(&fakeAuthorizer{decision: domain.AuthorizationDecision{Allowed: true}}, false); err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithPrincipal(context.Background(), soloDeveloper())
	input := DecisionInput{KnowledgeID: id, ExpectedVersion: 1, Decision: "approve", Reason: "Reviewed exact evidence", IdempotencyKey: "test"}
	if _, err := svc.DecideKnowledge(ctx, input); !errors.Is(err, domain.ErrValidationRequired) {
		t.Fatalf("missing report: %v", err)
	}
	artifact, err := store.Put(ctx, []byte("fixture source"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	report, err := svc.RecordManualValidation(ctx, ValidationInput{KnowledgeID: id, ExpectedVersion: 1,
		SourceManifest: domain.SourceManifest{SchemaVersion: "hybrid-ai/knowledge-source/v1", Sources: []domain.KnowledgeSource{{Kind: "procedure", Reference: "fixture", ArtifactSHA256: artifact.SHA256}}},
		Criteria:       []domain.ValidationCriterion{{Name: "fixture", Passed: true, Observation: "Observed fixture"}}})
	if err != nil {
		t.Fatal(err)
	}
	input.ValidationID = report.ID
	if _, err := svc.DecideKnowledge(ctx, input); err != nil {
		t.Fatal(err)
	}
	if repo.decision.Actor != soloDeveloper().ID || repo.decision.RequestSHA256 == "" {
		t.Fatal("decision did not bind authenticated actor")
	}
	workload := soloDeveloper()
	workload.Human = false
	if _, err := svc.DecideKnowledge(identity.WithPrincipal(context.Background(), workload), input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("workload promotion: %v", err)
	}
	wrongProject := soloDeveloper()
	wrongProject.RoleBindings = map[string][]string{"another-project": {"product_owner"}}
	if _, err := svc.DecideKnowledge(identity.WithPrincipal(context.Background(), wrongProject), input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-project promotion: %v", err)
	}
	if repo.decisionCalls != 1 {
		t.Fatal("rejected requests reached persistence")
	}
}
