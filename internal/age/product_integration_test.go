package age

import (
	"context"
	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/authorization"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"github.com/dhanuka84/hybrid-ai-platform/migrations"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
)

func TestAGEProductProjectionIntegration(t *testing.T) {
	db := os.Getenv("TEST_AGE_DATABASE_URL")
	if db == "" {
		t.Skip("TEST_AGE_DATABASE_URL is not set")
	}
	ctx := context.Background()
	repo, err := postgres.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err = migrations.Apply(ctx, repo.Pool()); err != nil {
		t.Fatal(err)
	}
	id, _ := domain.NewID()
	project := "product-age-" + id
	principal := domain.PrincipalBootstrap{ID: "human:" + id, Human: true, Roles: []string{"development", "qa", "product_owner"}, ProjectIDs: []string{project}}
	if err = repo.BootstrapPrincipals(ctx, []domain.PrincipalBootstrap{principal}); err != nil {
		t.Fatal(err)
	}
	ctx = identity.WithPrincipal(ctx, principal.Principal())
	svc := service.New(repo, artifacts.NewLocalStore(t.TempDir()), nil, nil, true, false)
	if err = svc.ConfigureAuthorization(authorization.Disabled{}, false); err != nil {
		t.Fatal(err)
	}
	create := func(key, kind string) domain.ProductRecord {
		r, e := svc.PutProductRecord(ctx, domain.ProductRecordInput{ProjectID: project, ProductID: "checkout", Key: key, Kind: kind, Title: key, Content: "Synthetic order processing requirement", Classification: "internal", SourceID: "fixture", SourceRevision: "fixture-v1"})
		if e != nil {
			t.Fatal(e)
		}
		v, e := svc.ValidateProductRecord(ctx, service.ValidateProductInput{ProjectID: project, RecordID: r.ID, ExpectedSHA256: r.SHA256, Evidence: []string{"Synthetic fixture QA attestation"}})
		if e != nil {
			t.Fatal(e)
		}
		r, e = svc.DecideProductRecord(ctx, domain.ProductDecision{ProjectID: project, RecordID: r.ID, ExpectedSHA256: r.SHA256, Decision: "accept", Reason: "Explicit synthetic test approval", ValidationID: v.ID, IdempotencyKey: r.ID})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	brs, feature := create("BRS-1", "brs"), create("FEATURE-1", "feature")
	edge, err := svc.PutProductRelation(ctx, domain.ProductRelation{ProjectID: project, ProductID: "checkout", FromID: feature.ID, ToID: brs.ID, Kind: "implements", Evidence: "Synthetic accepted relation"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(repo.Pool(), repo, "product_knowledge_test")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err = store.ProjectProductRecord(ctx, feature, []domain.ProductRelation{edge}); err != nil {
		t.Fatal(err)
	}
	err = store.withAGE(ctx, func(tx pgx.Tx) error {
		params, _ := marshalParameters(map[string]any{"project": project})
		var count string
		err := tx.QueryRow(ctx, store.cypherSQL(`MATCH (a:ProductRecord)-[r:PRODUCT_RELATION]->(b:ProductRecord) WHERE a.project_id=$project RETURN count(r)`, `count ag_catalog.agtype`), params).Scan(&count)
		if err == nil && count != "1" {
			t.Errorf("product AGE edges=%s", count)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	stats, err := store.Rebuild(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ProductRecordsQueued < 2 {
		t.Fatal("AGE rebuild omitted eligible product records")
	}
}
