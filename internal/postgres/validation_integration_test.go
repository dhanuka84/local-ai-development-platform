package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/migrations"
)

// Synthetic identities and reports exercise storage invariants in a disposable
// database. They never constitute approval of real reusable knowledge.
func fixtureValidation(t *testing.T, ctx context.Context, r *Repository, item domain.KnowledgeItem, actor string, completed time.Time) domain.KnowledgeValidation {
	t.Helper()
	store := artifacts.NewLocalStore(t.TempDir())
	source, err := store.Put(ctx, []byte("synthetic procedure fixture"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	manifest := domain.SourceManifest{SchemaVersion: "hybrid-ai/knowledge-source/v1", Sources: []domain.KnowledgeSource{{Kind: "procedure", Reference: "synthetic-fixture", ArtifactSHA256: source.SHA256}}}
	manifestJSON, _ := json.Marshal(manifest)
	sourceArtifact, err := store.Put(ctx, manifestJSON, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := domain.NewID()
	report := domain.KnowledgeValidation{ID: id, SchemaVersion: domain.ValidationSchema, KnowledgeID: item.ID, ProjectID: item.ProjectID, CandidateVersion: item.Version,
		ContentSHA256: domain.Digest([]byte(item.Content)), SourceManifest: manifest, SourceManifestSHA256: sourceArtifact.SHA256, SourceArtifact: sourceArtifact,
		Method: "manual", Criteria: []domain.ValidationCriterion{{Name: "fixture", Passed: true, Observation: "synthetic test evidence"}}, Verdict: "pass", ValidatedBy: actor,
		StartedAt: completed.Add(-time.Second), CompletedAt: completed, ValidUntil: completed.Add(24 * time.Hour)}
	payload, _ := json.Marshal(report)
	report.ReportArtifact, err = store.Put(ctx, payload, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	report, err = r.RecordKnowledgeValidation(ctx, report)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestKnowledgePromotionTransactionIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	r, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := migrations.Apply(ctx, r.Pool()); err != nil {
		t.Fatal(err)
	}
	project, _ := domain.NewID()
	actor := "human:promotion-fixture-" + project
	if err := r.BootstrapPrincipals(ctx, []domain.PrincipalBootstrap{{ID: actor, Human: true, Roles: []string{"qa", "product_owner"}, ProjectIDs: []string{project}}}); err != nil {
		t.Fatal(err)
	}
	store := artifacts.NewLocalStore(t.TempDir())
	artifact, err := store.Put(ctx, []byte("synthetic fixture"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	create := func() domain.KnowledgeItem {
		id, _ := domain.NewID()
		item, err := r.RecordGeneration(ctx, domain.GenerationCapture{ID: id, ProjectID: project, Prompt: "fixture", Response: "fixture solution " + id, Summary: "synthetic summary", Procedure: []string{"execute synthetic check"}, PromptArtifact: artifact, OutputArtifact: artifact})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	decision := func(item domain.KnowledgeItem, validationID, key string) domain.KnowledgeDecision {
		id, _ := domain.NewID()
		return domain.KnowledgeDecision{ID: id, KnowledgeID: item.ID, ExpectedVersion: item.Version, ValidationID: validationID, Decision: "approve", Reason: "synthetic fixture decision", IdempotencyKey: key, Actor: actor, RequestSHA256: domain.Digest([]byte(item.ID + validationID + key)), Authorization: domain.AuthorizationDecision{Allowed: true, CallID: "synthetic-policy"}}
	}
	item := create()
	if _, err := r.ApproveCandidate(ctx, item.ID, actor); !errors.Is(err, domain.ErrValidationRequired) {
		t.Fatalf("legacy bypass: %v", err)
	}
	if _, err := r.Pool().Exec(ctx, `UPDATE knowledge_items SET status='approved',approved_by=$2 WHERE id=$1`, item.ID, actor); err == nil {
		t.Fatal("direct SQL approval bypassed evidence")
	}
	if _, err := r.DecideKnowledge(ctx, decision(item, "", "missing")); !errors.Is(err, domain.ErrValidationRequired) {
		t.Fatalf("missing validation: %v", err)
	}
	expired := fixtureValidation(t, ctx, r, item, actor, time.Now().Add(-48*time.Hour))
	if _, err := r.DecideKnowledge(ctx, decision(item, expired.ID, "expired")); !errors.Is(err, domain.ErrValidationRequired) {
		t.Fatalf("expired validation: %v", err)
	}
	valid := fixtureValidation(t, ctx, r, item, actor, time.Now().UTC())
	input := decision(item, valid.ID, "same-decision")
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := r.DecideKnowledge(ctx, input); results <- err }()
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var decisions, events int
	if err := r.Pool().QueryRow(ctx, `SELECT count(*) FROM knowledge_decisions WHERE knowledge_id=$1`, item.ID).Scan(&decisions); err != nil {
		t.Fatal(err)
	}
	if err := r.Pool().QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id=$1`, item.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if decisions != 1 || events != 1 {
		t.Fatalf("duplicate publication: decisions=%d events=%d", decisions, events)
	}
	input.RequestSHA256 = domain.Digest([]byte("different request"))
	if _, err := r.DecideKnowledge(ctx, input); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("idempotency conflict: %v", err)
	}
	if _, err := r.Pool().Exec(ctx, `UPDATE knowledge_validations SET verdict='fail' WHERE id=$1`, valid.ID); err == nil {
		t.Fatal("immutable validation modified")
	}
	changed := create()
	old := fixtureValidation(t, ctx, r, changed, actor, time.Now().UTC())
	if _, err := r.Pool().Exec(ctx, `UPDATE knowledge_items SET content='changed' WHERE id=$1`, changed.ID); err == nil {
		t.Fatal("unversioned content mutation accepted")
	}
	if _, err := r.Pool().Exec(ctx, `UPDATE knowledge_items SET content='changed',version=version+1 WHERE id=$1`, changed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DecideKnowledge(ctx, decision(changed, old.ID, "old-version")); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("changed version: %v", err)
	}
	regulated := create()
	regulatedReport := fixtureValidation(t, ctx, r, regulated, actor, time.Now().UTC())
	if _, err := r.Pool().Exec(ctx, `INSERT INTO project_governance_policies(project_id,profile,allow_role_overlap) VALUES($1,'regulated',false)`, project); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DecideKnowledge(ctx, decision(regulated, regulatedReport.ID, "same-human-regulated")); err == nil || !strings.Contains(err.Error(), "distinct") {
		t.Fatalf("regulated gate bypass: %v", err)
	}
	otherActor := actor + "-independent"
	if err := r.BootstrapPrincipals(ctx, []domain.PrincipalBootstrap{{ID: otherActor, Human: true, Roles: []string{"product_owner"}, ProjectIDs: []string{project}}}); err != nil {
		t.Fatal(err)
	}
	independent := decision(regulated, regulatedReport.ID, "independent-human")
	independent.Actor = otherActor
	if _, err := r.DecideKnowledge(ctx, independent); err != nil {
		t.Fatal(err)
	}
}
