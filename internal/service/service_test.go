package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type fakeRepository struct {
	recorded       domain.GenerationCapture
	recordedReview domain.ReviewRecord
	items          []domain.KnowledgeItem
	lexical        []domain.SearchHit
	relations      []domain.RepositoryRelation
	upsertRelation domain.RepositoryRelation
	codeSnapshot   codegraph.Snapshot
	codeEntities   []domain.CodeEntity
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
func (f *fakeRepository) RecordReview(_ context.Context, review domain.ReviewRecord) error {
	f.recordedReview = review
	return nil
}
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
func (f *fakeRepository) StoreCodeGraph(_ context.Context, projectID string, repository domain.SoftwareRepository, requestedBy string, snapshot codegraph.Snapshot) (domain.CodeAnalysis, error) {
	f.codeSnapshot = snapshot
	return domain.CodeAnalysis{ID: "analysis", ProjectID: projectID, Repository: repository, Revision: snapshot.Revision, RequestedBy: requestedBy}, nil
}
func (f *fakeRepository) GetCodeEntity(context.Context, string) (domain.CodeEntity, error) {
	return domain.CodeEntity{}, nil
}
func (f *fakeRepository) GetCodeEntitiesMany(context.Context, []string) ([]domain.CodeEntity, error) {
	return f.codeEntities, nil
}
func (f *fakeRepository) SearchCodeEntitiesLexical(context.Context, string, string, string, int) ([]domain.CodeEntity, error) {
	return f.codeEntities, nil
}
func (f *fakeRepository) GetCodeGraph(context.Context, string, string, string, int) (domain.CodeGraph, error) {
	return domain.CodeGraph{Entities: f.codeEntities}, nil
}
func (f *fakeRepository) RequeueCodeEntities(context.Context) (int64, error) { return 0, nil }

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
	codeHits     []domain.VectorHit
	codeRepo     string
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
func (f *fakeVectors) UpsertCodeEntity(context.Context, domain.CodeEntity, []float32) error {
	return nil
}
func (f *fakeVectors) SearchCodeEntities(_ context.Context, _, repositoryID string, _ []float32, _ int) ([]domain.VectorHit, error) {
	f.codeRepo = repositoryID
	return f.codeHits, nil
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

func TestRecordReviewStoresImmutableEvidence(t *testing.T) {
	repository := &fakeRepository{}
	artifacts := &fakeArtifacts{}
	svc := New(repository, artifacts, &fakeEmbedder{}, &fakeVectors{}, true, false)

	review, err := svc.RecordReview(context.Background(), domain.ReviewRecord{
		KnowledgeID: " candidate ", Reviewer: " codex ", Provider: " openai ", Model: " reviewer-model ",
		Verdict: " REVISE ", Comments: "summary", RawOutput: "exact remote output",
		ContextManifest: `{"revision":"abc123","paths":["internal/service/service.go"]}`,
		ImprovedContent: "validated improvement", ValidationEvidence: []string{" go test ./internal/service passed "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if artifacts.count != 2 || review.ReviewArtifact.SHA256 != "exact remote output" {
		t.Fatalf("review=%#v artifact count=%d", review, artifacts.count)
	}
	if repository.recordedReview.Verdict != "revise" || repository.recordedReview.KnowledgeID != "candidate" {
		t.Fatalf("recorded review=%#v", repository.recordedReview)
	}
	if repository.recordedReview.ContextManifestArtifact.SHA256 == "" {
		t.Fatal("context manifest artifact was not recorded")
	}
	if got := repository.recordedReview.ValidationEvidence; len(got) != 1 || got[0] != "go test ./internal/service passed" {
		t.Fatalf("validation evidence=%#v", got)
	}
}

func TestRecordReviewRejectsInvalidContextManifest(t *testing.T) {
	svc := New(&fakeRepository{}, &fakeArtifacts{}, &fakeEmbedder{}, &fakeVectors{}, true, false)
	_, err := svc.RecordReview(context.Background(), domain.ReviewRecord{
		KnowledgeID: "candidate", Reviewer: "kimi", Verdict: "comment", ContextManifest: "not-json",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid manifest error = %v", err)
	}
}

func TestRecordReviewRequiresFreshEvidenceForRevision(t *testing.T) {
	svc := New(&fakeRepository{}, &fakeArtifacts{}, &fakeEmbedder{}, &fakeVectors{}, true, false)
	_, err := svc.RecordReview(context.Background(), domain.ReviewRecord{
		KnowledgeID: "candidate", Reviewer: "codex", Verdict: "revise", ImprovedContent: "cloud suggestion",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing revision evidence error = %v", err)
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

type fakeCodeAnalyzer struct {
	request codegraph.Request
}

func (f *fakeCodeAnalyzer) Name() string    { return "fake" }
func (f *fakeCodeAnalyzer) Version() string { return "1" }
func (f *fakeCodeAnalyzer) Analyze(_ context.Context, request codegraph.Request) (codegraph.Snapshot, error) {
	f.request = request
	now := time.Now()
	return codegraph.Snapshot{
		RepositoryPath: request.RepositoryPath, Revision: "abc123", Analyzer: f.Name(), AnalyzerVersion: f.Version(),
		StartedAt: now, CompletedAt: now,
	}, nil
}

func TestIndexCodeRepositoryEnforcesAllowedRoot(t *testing.T) {
	root := t.TempDir()
	repositoryPath := filepath.Join(root, "repo")
	if err := os.Mkdir(repositoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{}
	analyzer := &fakeCodeAnalyzer{}
	svc := New(repository, &fakeArtifacts{}, &fakeEmbedder{}, &fakeVectors{}, true, false)
	if err := svc.ConfigureCodeGraph(analyzer, []string{root}, CodeGraphLimits{MaxFiles: 10, MaxEntities: 20, MaxRelations: 30}); err != nil {
		t.Fatal(err)
	}
	analysis, err := svc.IndexCodeRepository(context.Background(), CodeIndexInput{
		ProjectID: "product", RepositoryPath: repositoryPath, RequestedBy: "codex",
		Repository: domain.SoftwareRepository{Name: "repo", CanonicalURL: "https://example.test/repo.git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.ID != "analysis" || analyzer.request.MaxRelations != 30 || repository.codeSnapshot.Revision != "abc123" {
		t.Fatalf("analysis=%#v request=%#v", analysis, analyzer.request)
	}
	_, err = svc.IndexCodeRepository(context.Background(), CodeIndexInput{
		ProjectID: "product", RepositoryPath: filepath.Dir(root), RequestedBy: "codex",
		Repository: domain.SoftwareRepository{Name: "repo", CanonicalURL: "https://example.test/repo.git"},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("outside-root error = %v", err)
	}
}

func TestCodeSearchPreservesVectorRankingAndRepositoryFilter(t *testing.T) {
	repository := &fakeRepository{codeEntities: []domain.CodeEntity{
		{ID: "a", RepositoryID: "repo-1"}, {ID: "b", RepositoryID: "repo-2"},
	}}
	vectors := &fakeVectors{codeHits: []domain.VectorHit{{ID: "b", Score: .9}, {ID: "a", Score: .8}}}
	svc := New(repository, &fakeArtifacts{}, &fakeEmbedder{vectors: [][]float32{{1}}}, vectors, true, false)
	entities, backend, err := svc.SearchCodeEntities(context.Background(), "product", "repo-1", "save", 5)
	if err != nil {
		t.Fatal(err)
	}
	if backend != "milvus" || vectors.codeRepo != "repo-1" || len(entities) != 1 || entities[0].ID != "a" || entities[0].Score != .8 {
		t.Fatalf("backend=%s entities=%#v", backend, entities)
	}
}
