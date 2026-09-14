package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
	golanganalyzer "github.com/dhanuka84/hybrid-ai-platform/components/codegraph/golang"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

func TestSDLCProductCodeBridgeE2E(t *testing.T) {
	f := startRuntime(t)
	tokens, _ := configureExecutionFixture(t, f)
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(f.source, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.invalid/labels\n\ngo 1.26\n")
	write("module.go", "package labels\n\nfunc Normalize(s string) string { return s }\n")
	f.git("config", "user.name", "Synthetic fixture")
	f.git("config", "user.email", "fixture@example.invalid")
	f.git("add", "go.mod", "module.go")
	f.git("commit", "-m", "synthetic code bridge")
	f.revision = f.git("rev-parse", "HEAD")
	index := func() domain.CodeAnalysis {
		t.Helper()
		snapshot, err := golanganalyzer.New().Analyze(f.ctx, codegraph.Request{RepositoryPath: f.source, Revision: f.revision, MaxFiles: 10, MaxEntities: 100, MaxRelations: 100})
		if err != nil {
			t.Fatal(err)
		}
		analysis, err := f.repo.StoreCodeGraph(f.ctx, f.project, domain.SoftwareRepository{Name: "synthetic-repository", CanonicalURL: "https://example.invalid/" + f.project + "/labels"}, "human:local-developer", snapshot)
		if err != nil {
			t.Fatal(err)
		}
		return analysis
	}
	analysis := index()
	intent, _ := executionFixtureIntent(f)
	raw, _ := json.Marshal(domain.ProductCodeBinding{Schema: "hybrid-ai/product-code/v1", RepositoryID: "synthetic-repository", Revision: f.revision, File: "module.go", Symbols: []string{"Normalize"}})
	code := acceptExecutionFixture(f, "CODE-labels", "code", string(raw), 0)
	var spec domain.IntentSpecification
	if err := json.Unmarshal([]byte(intent.Content), &spec); err != nil {
		t.Fatal(err)
	}
	spec.Bindings = append(spec.Bindings, domain.ProductBinding{RecordID: code.ID, SHA256: code.SHA256, Kind: code.Kind})
	raw, _ = json.Marshal(spec)
	intent = acceptExecutionFixture(f, "INTENT-labels", "intent", string(raw), 1)
	var run domain.ExecutionRun
	f.call(f.operator, "sdlc_run_create", service.ExecutionCreateInput{ProjectID: f.project, TargetID: "synthetic-target", Kind: "feature", IntentID: intent.ID, ExpectedSHA256: intent.SHA256, IdempotencyKey: "code-bridge"}, true, &run)
	var claim domain.ExecutionClaim
	f.call(tokens["sdlc_builder"], "sdlc_step_claim", service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}, true, &claim)
	lease := domain.ExecutionLease{ProjectID: f.project, RunID: run.ID, StepID: claim.Step.ID, Fence: claim.Step.Fence, Token: claim.Token}
	var c service.ExecutionContext
	f.call(tokens["sdlc_builder"], "sdlc_step_context", lease, true, &c)
	if len(c.Code) != 1 || c.Code[0].Graph.Analysis.ID != analysis.ID || c.Code[0].Binding.Revision != f.revision {
		t.Fatal("authoritative symbol bridge missing")
	}
	linked := false
	for _, link := range c.Links {
		if link.From == code.ID && link.Relation == "implemented_by" {
			linked = true
		}
	}
	if !linked {
		t.Fatal("code bridge lacks lifecycle relationship")
	}
	write("module.go", "package labels\n\nfunc Normalize(s string) string { return s + \"changed\" }\n")
	f.git("add", "module.go")
	f.git("commit", "-m", "advance synthetic code head")
	f.revision = f.git("rev-parse", "HEAD")
	index()
	f.call(tokens["sdlc_builder"], "sdlc_step_context", lease, false, nil)
	f.call(tokens["sdlc_builder"], "sdlc_artifact_put", struct {
		domain.ExecutionLease
		Content   string `json:"content"`
		MediaType string `json:"media_type"`
	}{lease, "stale proposal", "text/plain"}, false, nil)
}
