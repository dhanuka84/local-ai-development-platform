package service

import (
	"context"
	"errors"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type fakeRepository struct {
	recorded       domain.GenerationCapture
	items          []domain.KnowledgeItem
	lexical        []domain.SearchHit
	relations      []domain.RepositoryRelation
	upsertRelation domain.RepositoryRelation
}

func (f *fakeRepository) Ping(context.Context) error { return nil }
func (f *fakeRepository) RecordGeneration(_ context.Context, capture domain.GenerationCapture) (domain.KnowledgeItem, error) {
	f.recorded = capture
	return domain.KnowledgeItem{ID: "candidate", ProjectID: capture.ProjectID, Status: domain.CandidatePending}, nil
}
func (f *fakeRepository) GetKnowledge(context.Context, string, bool) (domain.KnowledgeItem, error) {
	return domain.KnowledgeItem{}, nil
}
func (f *fakeRepository) GetKnowledgeMany(context.Context, []string) ([]domain.KnowledgeItem, error) {
	return f.items, nil
}
func (f *fakeRepository) SearchApprovedLexical(context.Context, string, string, int) ([]domain.SearchHit, error) {
	return f.lexical, nil
}
func (f *fakeRepository) ListCandidates(context.Context, string, int) ([]domain.KnowledgeItem, error) {
	return nil, nil
}
func (f *fakeRepository) ApproveCandidate(context.Context, string, string) (domain.KnowledgeItem, error) {
	return domain.KnowledgeItem{}, nil
}
func (f *fakeRepository) RejectCandidate(context.Context, string, string) (domain.KnowledgeItem, error) {
	return domain.KnowledgeItem{}, nil
}
func (f *fakeRepository) RecordReview(context.Context, domain.ReviewRecord) error { return nil }
func (f *fakeRepository) ClaimOutbox(context.Context, int) ([]domain.OutboxEvent, error) {
	return nil, nil
}
func (f *fakeRepository) CompleteOutbox(context.Context, int64) error     { return nil }
func (f *fakeRepository) FailOutbox(context.Context, int64, string) error { return nil }
func (f *fakeRepository) RequeueApprovedKnowledge(context.Context) (int64, error) {
	return 0, nil
}
func (f *fakeRepository) RequeueRepositoryRelations(context.Context) (int64, error) {
	return 0, nil
}
func (f *fakeRepository) UpsertRepositoryRelation(_ context.Context, relation domain.RepositoryRelation) (domain.RepositoryRelation, error) {
	f.upsertRelation = relation
	relation.ID = "relation"
	return relation, nil
}
func (f *fakeRepository) GetRepositoryRelation(context.Context, string) (domain.RepositoryRelation, error) {
	return domain.RepositoryRelation{}, nil
}
func (f *fakeRepository) GetRepositoryRelationsMany(context.Context, []string) ([]domain.RepositoryRelation, error) {
	return f.relations, nil
}
func (f *fakeRepository) GetRepositoryGraph(context.Context, string, string, int) ([]domain.RepositoryRelation, error) {
	return f.relations, nil
}

type fakeArtifacts struct{ count int }

func (f *fakeArtifacts) Put(_ context.Context, data []byte, _ string) (domain.Artifact, error) {
	f.count++
	return domain.Artifact{SHA256: string(data), URI: "file://test", SizeBytes: int64(len(data))}, nil
}

type fakeEmbedder struct {
	vectors [][]float32
	err     error
}

func (f *fakeEmbedder) Embed(context.Context, []string) ([][]float32, error) { return f.vectors, f.err }
func (f *fakeEmbedder) Ping(context.Context) error                           { return f.err }

type fakeVectors struct {
	hits         []domain.VectorHit
	relationHits []domain.VectorHit
}

func (f *fakeVectors) EnsureCollection(context.Context) error { return nil }
func (f *fakeVectors) Upsert(context.Context, domain.KnowledgeItem, []float32) error {
	return nil
}
func (f *fakeVectors) Search(context.Context, string, []float32, int) ([]domain.VectorHit, error) {
	return f.hits, nil
}
func (f *fakeVectors) UpsertRelation(context.Context, domain.RepositoryRelation, []float32) error {
	return nil
}
func (f *fakeVectors) SearchRelations(context.Context, string, []float32, int) ([]domain.VectorHit, error) {
	return f.relationHits, nil
}
func (f *fakeVectors) Ping(context.Context) error  { return nil }
func (f *fakeVectors) Close(context.Context) error { return nil }

func TestCapturePreservesReusableProcedure(t *testing.T) {
	repository := &fakeRepository{}
	artifacts := &fakeArtifacts{}
	svc := New(repository, artifacts, &fakeEmbedder{}, &fakeVectors{}, true, false)

	item, err := svc.Capture(context.Background(), CaptureInput{
		ProjectID: " product ", Prompt: " fix it ", Response: " done ",
		Procedure: []string{" inspect ", "", " test "}, ValidationEvidence: []string{" go test ./... "},
		Tags: []string{"Go", "go", " MCP "}, RepositoryRevision: "abc123", Outcome: "success",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != "candidate" || artifacts.count != 2 {
		t.Fatalf("unexpected capture result: %#v, artifact count %d", item, artifacts.count)
	}
	if got := repository.recorded.Procedure; len(got) != 2 || got[0] != "inspect" || got[1] != "test" {
		t.Fatalf("procedure = %#v", got)
	}
	if got := repository.recorded.Tags; len(got) != 2 || got[0] != "go" || got[1] != "mcp" {
		t.Fatalf("tags = %#v", got)
	}
}

func TestSearchPreservesMilvusRankingAndApproval(t *testing.T) {
	repository := &fakeRepository{items: []domain.KnowledgeItem{
		{ID: "a", Status: domain.CandidateApproved},
		{ID: "b", Status: domain.CandidateApproved},
		{ID: "pending", Status: domain.CandidatePending},
	}}
	vectors := &fakeVectors{hits: []domain.VectorHit{{ID: "b", Score: .9}, {ID: "pending", Score: .8}, {ID: "a", Score: .7}}}
	svc := New(repository, &fakeArtifacts{}, &fakeEmbedder{vectors: [][]float32{{1}}}, vectors, true, false)

	hits, backend, err := svc.Search(context.Background(), "project", "query", 5)
	if err != nil {
		t.Fatal(err)
	}
	if backend != "milvus" || len(hits) != 2 || hits[0].ID != "b" || hits[1].ID != "a" {
		t.Fatalf("backend=%s hits=%#v", backend, hits)
	}
}

func TestSearchFallsBackToPostgres(t *testing.T) {
	repository := &fakeRepository{lexical: []domain.SearchHit{{KnowledgeItem: domain.KnowledgeItem{ID: "fallback"}}}}
	svc := New(repository, &fakeArtifacts{}, &fakeEmbedder{err: errors.New("offline")}, &fakeVectors{}, true, false)
	hits, backend, err := svc.Search(context.Background(), "project", "query", 5)
	if err != nil {
		t.Fatal(err)
	}
	if backend != "postgres-lexical-fallback" || len(hits) != 1 {
		t.Fatalf("backend=%s hits=%#v", backend, hits)
	}
}

func TestRepositoryRelationRequiresEvidenceAndNormalizesType(t *testing.T) {
	repository := &fakeRepository{}
	svc := New(repository, &fakeArtifacts{}, &fakeEmbedder{}, &fakeVectors{}, true, false)
	_, err := svc.UpsertRepositoryRelation(context.Background(), domain.RepositoryRelation{
		ProjectID: "product", From: domain.SoftwareRepository{Name: "api", CanonicalURL: "https://example/api.git"},
		To:           domain.SoftwareRepository{Name: "web", CanonicalURL: "https://example/web.git"},
		RelationType: " PROVIDES_API_TO ", Evidence: "web imports api client", ApprovedBy: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.upsertRelation.RelationType != "provides_api_to" {
		t.Fatalf("relation type = %q", repository.upsertRelation.RelationType)
	}
	_, err = svc.UpsertRepositoryRelation(context.Background(), domain.RepositoryRelation{
		ProjectID: "product", From: domain.SoftwareRepository{Name: "api", CanonicalURL: "a"},
		To: domain.SoftwareRepository{Name: "web", CanonicalURL: "b"}, RelationType: "depends_on", ApprovedBy: "owner",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing evidence error = %v", err)
	}
}
