package age

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
	"github.com/dhanuka84/hybrid-ai-platform/migrations"
)

func TestAGEProjectionAndTraversalIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_AGE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_AGE_DATABASE_URL is not set")
	}
	ctx := context.Background()
	repository, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	store, err := New(repository.Pool(), repository, "software_knowledge_graph_test")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, repository.Pool()); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Pool().Exec(ctx, `TRUNCATE projects CASCADE; TRUNCATE outbox_events RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}

	relationAB := upsertTestRelation(t, ctx, repository, "api", "web", "provides_api_to")
	relationBC := upsertTestRelation(t, ctx, repository, "web", "deploy", "deploys_with")
	_, err = store.ExpandRepositoryGraph(ctx, domain.RepositoryGraphRequest{
		ProjectID: "product", Root: relationAB.From.ID, MaxHops: 2,
	})
	if !errors.Is(err, ErrProjectionStale) {
		t.Fatalf("unprojected graph error = %v", err)
	}
	if err := store.ProjectRepositoryRelation(ctx, relationAB); err != nil {
		t.Fatal(err)
	}
	if err := store.ProjectRepositoryRelation(ctx, relationBC); err != nil {
		t.Fatal(err)
	}
	relations, err := store.ExpandRepositoryGraph(ctx, domain.RepositoryGraphRequest{
		ProjectID: "product", Root: relationAB.From.ID, MaxHops: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(relations) != 2 {
		t.Fatalf("AGE repository relations = %#v", relations)
	}
	relationalStore := postgres.NewRecursiveGraphStore(repository)
	relationalRelations, err := relationalStore.ExpandRepositoryGraph(ctx, domain.RepositoryGraphRequest{
		ProjectID: "product", Root: relationAB.From.ID, MaxHops: 2,
	})
	if err != nil || len(relationalRelations) != len(relations) {
		t.Fatalf("repository traversal mismatch: age=%d postgres=%d err=%v", len(relations), len(relationalRelations), err)
	}

	now := time.Now().UTC()
	snapshot := codegraph.Snapshot{
		RepositoryPath: "/tmp/api", RepositoryName: "api", Branch: "main", Revision: "abc123",
		Analyzer: "integration", AnalyzerVersion: "1", StartedAt: now, CompletedAt: now,
		Entities: []codegraph.Entity{
			{Key: "go:repository:.", Language: "go", Kind: codegraph.EntityRepository, Name: "api", QualifiedName: "."},
			{Key: "go:file:store.go", Language: "go", Kind: codegraph.EntityFile, Name: "store.go", QualifiedName: "store.go"},
			{Key: "go:function:example/api.Save", Language: "go", Kind: codegraph.EntityFunction, Name: "Save", QualifiedName: "example/api.Save"},
		},
		Relations: []codegraph.Relation{
			{SourceKey: "go:repository:.", TargetKey: "go:file:store.go", Kind: codegraph.RelationContains, Confidence: 1},
			{SourceKey: "go:file:store.go", TargetKey: "go:function:example/api.Save", Kind: codegraph.RelationDefines, Confidence: 1},
		},
	}
	analysis, err := repository.StoreCodeGraph(ctx, "product", domain.SoftwareRepository{
		Name: "api", CanonicalURL: "https://git.example/api.git", DefaultBranch: "main",
	}, "integration", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ProjectCodeGraph(ctx, analysis.ID); err != nil {
		t.Fatal(err)
	}
	codeResult, err := store.ExpandCodeGraph(ctx, domain.CodeGraphRequest{
		ProjectID: "product", RepositoryRoot: analysis.Repository.ID, SymbolRoot: "example/api.Save", MaxHops: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(codeResult.Entities) != 3 || len(codeResult.Relations) != 2 {
		t.Fatalf("AGE code graph = %#v", codeResult)
	}
	relationalCode, err := relationalStore.ExpandCodeGraph(ctx, domain.CodeGraphRequest{
		ProjectID: "product", RepositoryRoot: analysis.Repository.ID, SymbolRoot: "example/api.Save", MaxHops: 2,
	})
	if err != nil || len(relationalCode.Entities) != len(codeResult.Entities) || len(relationalCode.Relations) != len(codeResult.Relations) {
		t.Fatalf("code traversal mismatch: age=%d/%d postgres=%d/%d err=%v", len(codeResult.Entities), len(codeResult.Relations), len(relationalCode.Entities), len(relationalCode.Relations), err)
	}

	var knowledgeA, knowledgeB string
	if err := repository.Pool().QueryRow(ctx, `INSERT INTO knowledge_items(
      project_id,title,problem,content,status,version,approved_at,approved_by
    ) VALUES('product','Milvus indexing','impact','validated procedure','approved',1,now(),'owner') RETURNING id::text`).Scan(&knowledgeA); err != nil {
		t.Fatal(err)
	}
	if err := repository.Pool().QueryRow(ctx, `INSERT INTO knowledge_items(
      project_id,title,problem,content,status,version,approved_at,approved_by
    ) VALUES('product','API contract','impact','validated contract','approved',1,now(),'owner') RETURNING id::text`).Scan(&knowledgeB); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Pool().Exec(ctx, `INSERT INTO knowledge_relations(from_id,to_id,relation_type,confidence)
      VALUES($1,$2,'related_to',0.9)`, knowledgeA, knowledgeB); err != nil {
		t.Fatal(err)
	}
	var saveEntityID string
	if err := repository.Pool().QueryRow(ctx, `SELECT id::text FROM code_entities
      WHERE repository_id=$1 AND stable_key='go:function:example/api.Save'`, analysis.Repository.ID).Scan(&saveEntityID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Pool().Exec(ctx, `INSERT INTO knowledge_code_references(
      knowledge_id,entity_id,analysis_run_id,role,evidence
    ) VALUES($1,$2,$3,'applies_to','validated integration fixture')`, knowledgeA, saveEntityID, analysis.ID); err != nil {
		t.Fatal(err)
	}
	stats, err := store.Rebuild(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Repositories != 3 || stats.CodeEntities != 3 || stats.KnowledgeItems != 2 || stats.KnowledgeEdges != 2 {
		t.Fatalf("rebuild stats = %#v", stats)
	}
	contextGraph, err := store.ExpandKnowledgeGraph(ctx, domain.KnowledgeGraphRequest{
		ProjectID: "product", KnowledgeSeedIDs: []string{knowledgeA}, MaxHops: 3, MaxNodes: 20, MaxEdges: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if contextGraph.Backend != "apache-age" || len(contextGraph.Knowledge) != 2 || len(contextGraph.CodeEntities) == 0 || len(contextGraph.Edges) < 2 {
		t.Fatalf("AGE unified graph = %#v", contextGraph)
	}
	relationalContext, err := relationalStore.ExpandKnowledgeGraph(ctx, domain.KnowledgeGraphRequest{
		ProjectID: "product", KnowledgeSeedIDs: []string{knowledgeA}, MaxHops: 3, MaxNodes: 20, MaxEdges: 30,
	})
	if err != nil || len(relationalContext.Nodes) != len(contextGraph.Nodes) || len(relationalContext.Edges) != len(contextGraph.Edges) {
		t.Fatalf("unified traversal mismatch: age=%d/%d postgres=%d/%d err=%v", len(contextGraph.Nodes), len(contextGraph.Edges), len(relationalContext.Nodes), len(relationalContext.Edges), err)
	}
}

func upsertTestRelation(t *testing.T, ctx context.Context, repository *postgres.Repository, from, to, relationType string) domain.RepositoryRelation {
	t.Helper()
	relation, err := repository.UpsertRepositoryRelation(ctx, domain.RepositoryRelation{
		ProjectID:    "product",
		From:         domain.SoftwareRepository{Name: from, CanonicalURL: "https://git.example/" + from + ".git", DefaultBranch: "main", Revision: from + "-rev"},
		To:           domain.SoftwareRepository{Name: to, CanonicalURL: "https://git.example/" + to + ".git", DefaultBranch: "main", Revision: to + "-rev"},
		RelationType: relationType, Evidence: "integration fixture", Confidence: 1, ApprovedBy: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	return relation
}
