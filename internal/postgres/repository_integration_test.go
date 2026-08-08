package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
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
	for _, event := range events {
		if err := repository.CompleteOutbox(ctx, event.ID); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now().UTC()
	snapshot := codegraph.Snapshot{
		RepositoryPath: "/tmp/api", Revision: "abc", Analyzer: "integration", AnalyzerVersion: "1",
		StartedAt: now, CompletedAt: now,
		Entities: []codegraph.Entity{
			{Key: "go:repository:.", Language: "go", Kind: codegraph.EntityRepository, Name: "api", QualifiedName: "."},
			{Key: "go:file:store.go", Language: "go", Kind: codegraph.EntityFile, Name: "store.go", QualifiedName: "store.go"},
			{Key: "go:function:example/api.Save", Language: "go", Kind: codegraph.EntityFunction, Name: "Save", QualifiedName: "example/api.Save", Signature: "func Save()", Location: codegraph.Location{FilePath: "store.go", Start: codegraph.Position{Line: 3, Column: 1}, End: codegraph.Position{Line: 5, Column: 2}}},
		},
		Relations: []codegraph.Relation{
			{SourceKey: "go:repository:.", TargetKey: "go:file:store.go", Kind: codegraph.RelationContains, Confidence: 1},
			{SourceKey: "go:file:store.go", TargetKey: "go:function:example/api.Save", Kind: codegraph.RelationDefines, Confidence: 1},
		},
	}
	analysis, err := repository.StoreCodeGraph(ctx, "product", domain.SoftwareRepository{
		Name: "api", CanonicalURL: "https://git.example/api.git", DefaultBranch: "main",
	}, "codex", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.EntityCount != 3 || analysis.RelationCount != 2 || analysis.Repository.ID == "" {
		t.Fatalf("analysis = %#v", analysis)
	}
	symbols, err := repository.SearchCodeEntitiesLexical(ctx, "product", analysis.Repository.ID, "Save", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 1 || symbols[0].QualifiedName != "example/api.Save" {
		t.Fatalf("symbols = %#v", symbols)
	}
	codeGraph, err := repository.GetCodeGraph(ctx, "product", analysis.Repository.ID, "example/api.Save", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(codeGraph.Entities) != 3 || len(codeGraph.Relations) != 2 {
		t.Fatalf("code graph = %#v", codeGraph)
	}
	codeEvents, err := repository.ClaimOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(codeEvents) != 1 || codeEvents[0].Topic != "code_entity.upsert" {
		t.Fatalf("code outbox events = %#v", codeEvents)
	}
	if codeEvents[0].AggregateID != symbols[0].ID {
		t.Fatalf("Milvus projection ID %q does not match PostgreSQL entity ID %q", codeEvents[0].AggregateID, symbols[0].ID)
	}
	if err := repository.CompleteOutbox(ctx, codeEvents[0].ID); err != nil {
		t.Fatal(err)
	}

	// A later revision reuses the logical entity UUID. Milvus therefore
	// overwrites the same primary key while PostgreSQL advances the active head.
	snapshot.Revision = "def"
	snapshot.StartedAt = now.Add(time.Minute)
	snapshot.CompletedAt = now.Add(time.Minute)
	for index := range snapshot.Entities {
		if snapshot.Entities[index].Key == "go:function:example/api.Save" {
			snapshot.Entities[index].Signature = "func Save(ctx context.Context)"
		}
	}
	secondAnalysis, err := repository.StoreCodeGraph(ctx, "product", domain.SoftwareRepository{
		Name: "api", CanonicalURL: "https://git.example/api.git", DefaultBranch: "main",
	}, "codex", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if secondAnalysis.ID == analysis.ID || secondAnalysis.Revision != "def" {
		t.Fatalf("second analysis = %#v", secondAnalysis)
	}
	updated, err := repository.SearchCodeEntitiesLexical(ctx, "product", secondAnalysis.Repository.ID, "Save", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].ID != symbols[0].ID || updated[0].Revision != "def" ||
		updated[0].Signature != "func Save(ctx context.Context)" {
		t.Fatalf("stable entity update = %#v", updated)
	}
	updatedEvents, err := repository.ClaimOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(updatedEvents) != 1 || updatedEvents[0].AggregateID != symbols[0].ID {
		t.Fatalf("updated projection events = %#v", updatedEvents)
	}
}
