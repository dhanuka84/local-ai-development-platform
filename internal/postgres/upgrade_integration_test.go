package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Rehearse the deployed v7 boundary in a separate disposable database. Historical
// approval must survive migration without acquiring invented validation or
// eligibility. Never point TEST_DATABASE_URL at a live server.
func TestUpgradeFromV7PreservesLegacyEvidenceIntegration(t *testing.T) {
	endpoint := os.Getenv("TEST_DATABASE_URL")
	if endpoint == "" {
		t.Skip("requires disposable TEST_DATABASE_URL and database creation rights")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("agent_ready_upgrade_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+quoted); err != nil {
			t.Errorf("remove disposable upgrade database: %v", err)
		}
	}()
	config := admin.Config().Copy()
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, "CREATE TABLE schema_migrations(version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("../../migrations/00000[1-7]_*.sql")
	if err != nil || len(files) != 7 {
		t.Fatalf("expected seven baseline migrations: %v %v", files, err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, string(data)); err != nil {
			t.Fatalf("baseline %s: %v", file, err)
		}
		if _, err = pool.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", filepath.Base(file)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO projects(id,display_name) VALUES('upgrade-fixture','Synthetic upgrade');
		INSERT INTO knowledge_items(project_id,title,problem,summary,content,procedure,status,approved_by)
		VALUES ('upgrade-fixture','pending','fixture','fixture','pending source',ARRAY['fixture'],'pending',NULL),
		       ('upgrade-fixture','historical','fixture','fixture','historical source',ARRAY['fixture'],'approved','human:historical-fixture');
	`); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err = migrations.Apply(ctx, pool); err != nil {
			t.Fatalf("upgrade/repeat %d: %v", attempt, err)
		}
	}
	var preserved, eligible, validations, decisions int
	err = pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE
		 (title='pending' AND status='pending' AND approved_by IS NULL AND content='pending source') OR
		 (title='historical' AND status='approved' AND approved_by='human:historical-fixture' AND content='historical source')),
		count(*) FILTER (WHERE knowledge_eligible(id))
		FROM knowledge_items`).Scan(&preserved, &eligible)
	if err != nil || preserved != 2 || eligible != 0 {
		t.Fatalf("historical state/eligibility: preserved=%d eligible=%d err=%v", preserved, eligible, err)
	}
	err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM knowledge_validations), (SELECT count(*) FROM knowledge_decisions)`).Scan(&validations, &decisions)
	if err != nil || validations != 0 || decisions != 0 {
		t.Fatalf("migration invented evidence: validations=%d decisions=%d err=%v", validations, decisions, err)
	}
	var reason string
	if err = pool.QueryRow(ctx, "SELECT knowledge_quality_reason(id) FROM knowledge_items WHERE title='historical'").Scan(&reason); err != nil || reason != "validation_required" {
		t.Fatalf("legacy publication must await validation: %q %v", reason, err)
	}
}
