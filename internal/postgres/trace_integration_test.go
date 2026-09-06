package postgres

import (
	"context"
	"fmt"
	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
	"github.com/dhanuka84/hybrid-ai-platform/migrations"
	"os"
	"strings"
	"testing"
)

func TestEvidenceTransactionAndUnknownOutcomeIntegration(t *testing.T) {
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
	if err = migrations.Apply(ctx, r.Pool()); err != nil {
		t.Fatal(err)
	}
	project, _ := domain.NewID()
	constraint := "fixture_evidence_" + strings.ReplaceAll(project, "-", "")
	_, err = r.Pool().Exec(ctx, fmt.Sprintf("ALTER TABLE operation_records ADD CONSTRAINT %s CHECK(project_id<>'%s') NOT VALID", constraint, project))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = r.Pool().Exec(ctx, "ALTER TABLE operation_records DROP CONSTRAINT IF EXISTS "+constraint)
	}()
	store := artifacts.NewLocalStore(t.TempDir())
	artifact, err := store.Put(ctx, []byte("synthetic evidence fixture"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := domain.NewID()
	if _, err = r.RecordGeneration(ctx, domain.GenerationCapture{ID: id, ProjectID: project, Prompt: "fixture", Response: "fixture", Summary: "fixture", Procedure: []string{"fixture"}, PromptArtifact: artifact, OutputArtifact: artifact}); err == nil {
		t.Fatal("mutation succeeded despite failed audit")
	}
	var count int
	if err = r.Pool().QueryRow(ctx, `SELECT count(*) FROM generations WHERE id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed audit left generation committed")
	}
	if _, err = r.Pool().Exec(ctx, "ALTER TABLE operation_records DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	_, span, record := telemetry.Start(ctx, "external.fixture", domain.OperationScope{ProjectID: project, WorkflowID: "fixture"}, nil)
	span.End()
	if err = r.AppendOperation(ctx, record); err != nil {
		t.Fatal(err)
	}
	trace, err := r.ReadWorkflowTrace(ctx, project, "fixture", 100)
	if err != nil || trace.Complete || len(trace.Missing) == 0 {
		t.Fatalf("unknown outcome not surfaced: %+v %v", trace, err)
	}
	if _, err = r.Pool().Exec(ctx, `UPDATE operation_records SET outcome='success' WHERE id=$1`, record.ID); err == nil {
		t.Fatal("immutable evidence was rewritten")
	}
}
