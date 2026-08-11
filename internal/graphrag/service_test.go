package graphrag

import (
	"context"
	"errors"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type repositoryFake struct {
	domain.Repository
	knowledge []domain.KnowledgeItem
	code      []domain.CodeEntity
	lexical   []domain.SearchHit
}

func (f *repositoryFake) GetKnowledgeMany(context.Context, []string) ([]domain.KnowledgeItem, error) {
	return f.knowledge, nil
}
func (f *repositoryFake) GetCodeEntitiesMany(context.Context, []string) ([]domain.CodeEntity, error) {
	return f.code, nil
}
func (f *repositoryFake) GetRepositoryRelationsMany(context.Context, []string) ([]domain.RepositoryRelation, error) {
	return nil, nil
}
func (f *repositoryFake) GetSemanticGraphEdgesMany(context.Context, []string) ([]domain.SemanticGraphEdge, error) {
	return nil, nil
}
func (f *repositoryFake) SearchApprovedLexical(context.Context, string, string, int) ([]domain.SearchHit, error) {
	return f.lexical, nil
}
func (f *repositoryFake) SearchCodeEntitiesLexical(context.Context, string, string, string, int) ([]domain.CodeEntity, error) {
	return f.code, nil
}

type embedderFake struct {
	domain.Embedder
	err error
}

func (f *embedderFake) Embed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1, 2, 3}}, f.err
}

type vectorsFake struct {
	domain.VectorStore
	knowledge []domain.VectorHit
	code      []domain.VectorHit
}

func (f *vectorsFake) Search(context.Context, string, []float32, int) ([]domain.VectorHit, error) {
	return f.knowledge, nil
}
func (f *vectorsFake) SearchCodeEntities(context.Context, string, string, []float32, int) ([]domain.VectorHit, error) {
	return f.code, nil
}
func (f *vectorsFake) SearchRelations(context.Context, string, []float32, int) ([]domain.VectorHit, error) {
	return nil, nil
}
func (f *vectorsFake) SearchGraphEdges(context.Context, string, string, []float32, int) ([]domain.VectorHit, error) {
	return nil, nil
}

type graphStoreFake struct {
	domain.GraphStore
	request domain.KnowledgeGraphRequest
}

func (f *graphStoreFake) ExpandKnowledgeGraph(_ context.Context, request domain.KnowledgeGraphRequest) (domain.KnowledgeSubgraph, error) {
	f.request = request
	return domain.KnowledgeSubgraph{Backend: "apache-age", Nodes: []domain.GraphNode{
		{ID: "knowledge", Type: domain.GraphNodeKnowledgeItem, Distance: 0},
		{ID: "code", Type: domain.GraphNodeCodeEntity, Distance: 0},
		{ID: "expanded", Type: domain.GraphNodeCodeEntity, Distance: 1},
	}}, nil
}

func TestSearchUsesHydratedMilvusSeedsAndRanksExpansion(t *testing.T) {
	repository := &repositoryFake{
		knowledge: []domain.KnowledgeItem{{ID: "knowledge", ProjectID: "product", Status: domain.CandidateApproved}},
		code:      []domain.CodeEntity{{ID: "code", ProjectID: "product", RepositoryID: "repo", RepositoryName: "api"}},
	}
	vectors := &vectorsFake{
		knowledge: []domain.VectorHit{{ID: "knowledge", Score: .9}},
		code:      []domain.VectorHit{{ID: "code", Score: .8}},
	}
	graphs := &graphStoreFake{}
	service, err := New(repository, &embedderFake{}, vectors, graphs)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Search(context.Background(), Request{
		ProjectID: " product ", Query: " impact ", Repository: "api", SeedLimit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SeedBackend != "milvus" || result.Graph.Backend != "apache-age" ||
		len(graphs.request.KnowledgeSeedIDs) != 1 || len(graphs.request.CodeSeedIDs) != 1 {
		t.Fatalf("result=%#v request=%#v", result, graphs.request)
	}
	if result.Graph.Nodes[0].ID != "knowledge" || result.Graph.Nodes[2].Score <= 0 {
		t.Fatalf("ranked nodes = %#v", result.Graph.Nodes)
	}
}

func TestSearchFallsBackToPostgresLexicalSeeds(t *testing.T) {
	repository := &repositoryFake{lexical: []domain.SearchHit{{
		KnowledgeItem: domain.KnowledgeItem{ID: "knowledge", ProjectID: "product", Status: domain.CandidateApproved}, Score: .5,
	}}}
	graphs := &graphStoreFake{}
	service, err := New(repository, &embedderFake{err: errors.New("ollama unavailable")}, &vectorsFake{}, graphs)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Search(context.Background(), Request{ProjectID: "product", Query: "impact"})
	if err != nil {
		t.Fatal(err)
	}
	if result.SeedBackend != "postgres-lexical-fallback" || len(graphs.request.KnowledgeSeedIDs) != 1 {
		t.Fatalf("result=%#v request=%#v", result, graphs.request)
	}
}
