package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
)

func exerciseTextQuality(t *testing.T, s *Service, r *postgres.Repository, ctx context.Context, approved domain.KnowledgeItem, validation string) {
	t.Helper()
	item, err := s.Capture(ctx, CaptureInput{ProjectID: approved.ProjectID, WorkflowID: approved.WorkflowID, Prompt: "Synthetic duplicate inspection", Response: strings.ToUpper(approved.Content) + "\n", Summary: "Synthetic duplicate candidate", Provider: "ollama", Model: "synthetic-deterministic-fixture", TaskType: "maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.InspectCandidateQuality(ctx, item.ID, item.Version)
	if err != nil || len(result.PossibleDuplicates) != 1 || result.PossibleDuplicates[0].KnowledgeID != approved.ID || result.PossibleDuplicates[0].Version != approved.Version || result.PossibleDuplicates[0].ContentSHA256 != domain.Digest([]byte(approved.Content)) {
		t.Fatal("authoritative duplicate evidence missing", err, result.PossibleDuplicates)
	}
	if _, err = s.InspectCandidateQuality(ctx, item.ID, item.Version+1); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatal("stale candidate version accepted", err)
	}
	p, _ := identity.PrincipalFromContext(ctx)
	p.RoleBindings = map[string][]string{"different-project": {"development"}}
	if leaked, err := s.InspectCandidateQuality(identity.WithPrincipal(ctx, p), item.ID, item.Version); err == nil || leaked.KnowledgeID != "" {
		t.Fatal("cross-project inspection disclosed candidate")
	}
	current, err := r.GetKnowledge(ctx, item.ID, true)
	if err != nil || current.Status != domain.CandidatePending || current.Version != item.Version {
		t.Fatal("inspection mutated or approved candidate", err)
	}
	if err = r.RecordSourceCheck(ctx, approved.ID, approved.Version, validation, false, "source_unavailable"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.RefreshKnowledgeSources(ctx, approved.ID); err != nil {
			t.Error("restore actual source verification", err)
		}
	}()
	result, err = s.InspectCandidateQuality(ctx, item.ID, item.Version)
	if err != nil || len(result.PossibleDuplicates) != 0 {
		t.Fatal("ineligible source was returned as duplicate guidance", err)
	}
}
