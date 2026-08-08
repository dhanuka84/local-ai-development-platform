package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/migrations"
)

func TestRepositoryWorkflowIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	repository, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	if err := migrations.Apply(ctx, repository.Pool()); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Pool().Exec(ctx, `TRUNCATE projects CASCADE; TRUNCATE outbox_events RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}

	generationID, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	artifactA := domain.Artifact{SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", URI: "file:///a", MediaType: "text/plain", SizeBytes: 7}
	artifactB := domain.Artifact{SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", URI: "file:///b", MediaType: "text/plain", SizeBytes: 8}
	candidate, err := repository.RecordGeneration(ctx, domain.GenerationCapture{
		ID: generationID, ProjectID: "product", SessionID: "session", TaskType: "debugging",
		Prompt: "fix repository contract mismatch", Response: "original solution", Summary: "repair shared contract",
		Procedure: []string{"inspect schema", "update client"}, ValidationEvidence: []string{"go test ./... passed"},
		Provider: "local", Model: "test", PromptArtifact: artifactA, OutputArtifact: artifactB,
	})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != domain.CandidatePending || candidate.Problem == "" || len(candidate.Procedure) != 2 {
		t.Fatalf("candidate = %#v", candidate)
	}
	reviewID, _ := domain.NewID()
	if err := repository.RecordReview(ctx, domain.ReviewRecord{
		ID: reviewID, KnowledgeID: candidate.ID, Reviewer: "codex", Provider: "openai", Model: "review-model",
		Verdict: "revise", Comments: "make validation explicit", ImprovedContent: "reviewed solution",
	}); err != nil {
		t.Fatal(err)
	}
	revised, err := repository.GetKnowledge(ctx, candidate.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if revised.Content != "reviewed solution" || revised.Version != 2 {
		t.Fatalf("revised candidate = %#v", revised)
	}
	approved, err := repository.ApproveCandidate(ctx, candidate.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != domain.CandidateApproved || approved.ApprovedAt.IsZero() {
		t.Fatalf("approved = %#v", approved)
	}

	relation, err := repository.UpsertRepositoryRelation(ctx, domain.RepositoryRelation{
		ProjectID:    "product",
		From:         domain.SoftwareRepository{Name: "api", CanonicalURL: "https://git.example/api.git", DefaultBranch: "main", Revision: "abc"},
		To:           domain.SoftwareRepository{Name: "web", CanonicalURL: "https://git.example/web.git", DefaultBranch: "main", Revision: "def"},
		RelationType: "provides_api_to", Evidence: "web/go.mod imports api client", Confidence: .95, ApprovedBy: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := repository.GetRepositoryGraph(ctx, "product", relation.From.CanonicalURL, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph) != 1 || graph[0].ID != relation.ID {
		t.Fatalf("graph = %#v", graph)
	}

	events, err := repository.ClaimOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("outbox events = %#v", events)
	}
}
