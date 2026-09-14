//go:build sdlc_host

package e2e

import (
	"encoding/json"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"os"
	"path/filepath"
	"testing"
)

func TestSDLCLocalIntentAndOperatorCLIE2E(t *testing.T) {
	f := startRuntime(t)
	_, registry := configureExecutionFixture(t, f)
	accepted, _ := executionFixtureIntent(f)
	spec, err := domain.ParseIntent(accepted.Content)
	if err != nil {
		t.Fatal(err)
	}
	credential := f.write("operator-cli.token", []byte(f.operator))
	invoke := func(args ...string) []byte {
		t.Helper()
		full := append([]string{"--project", f.project, "--token-file", credential, "--url", f.endpoint + "/mcp"}, args...)
		cmd := f.command("sdlc", "", full...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
		raw, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("operator CLI: %v %s", e, raw)
		}
		return raw
	}
	pkg := registry.Packages[0]
	evidence := filepath.Join(os.Getenv("AGENT_READY_REPORT_DIR"), t.Name(), "intake")
	input := map[string]any{"product_id": "labels", "key": "INTENT-cli", "expected_version": 0, "goal": spec.Goal, "bindings": spec.Bindings, "answers": map[string]string{}, "package": pkg, "ollama_url": f.env["OLLAMA_URL"], "evidence_directory": evidence}
	reply := func(proposal any) {
		raw, _ := json.Marshal(proposal)
		response, _ := json.Marshal(execution.ModelResponse{Model: pkg.Model, Response: string(raw), Done: true, DoneReason: "stop", PromptEvalCount: 200, EvalCount: 100})
		f.generationResponses <- response
	}
	reply(map[string]any{"intent": nil, "clarifications": []string{"Confirm whether whitespace normalization preserves word separation."}})
	path := f.writeJSON("intake.json", input)
	var missing struct {
		Status         string   `json:"status"`
		Clarifications []string `json:"clarifications"`
	}
	if json.Unmarshal(invoke("draft", path), &missing) != nil || missing.Status != "clarification_required" || len(missing.Clarifications) != 1 {
		t.Fatal("intake did not surface a missing decision")
	}
	input["answers"] = map[string]string{"whitespace": "Preserve one space between words as required by BRS."}
	path = f.writeJSON("intake-answered.json", input)
	reply(map[string]any{"intent": spec, "clarifications": []string{}})
	var drafted struct {
		Record   domain.ProductRecord `json:"record"`
		Status   string               `json:"status"`
		Evidence []domain.Artifact    `json:"evidence"`
	}
	if json.Unmarshal(invoke("draft", path), &drafted) != nil || drafted.Status != "pending" || drafted.Record.ID == "" || len(drafted.Evidence) != 3 {
		t.Fatal("local intent proposal or immutable disclosure missing")
	}
	create := service.ExecutionCreateInput{ProjectID: f.project, TargetID: registry.Targets[0].ID, Kind: "feature", IntentID: drafted.Record.ID, ExpectedSHA256: drafted.Record.SHA256, IdempotencyKey: "cli-intent"}
	f.call(f.operator, "sdlc_run_create", create, false, nil)
	review := f.writeJSON("intent-review.json", map[string]any{"record_id": drafted.Record.ID, "sha256": drafted.Record.SHA256, "evidence": []string{"Synthetic operator checked the exact BRS bindings, answered question and protected criterion."}, "reason": "Explicit synthetic QA and product-owner decision"})
	_ = invoke("review", review)
	var run domain.ExecutionRun
	if json.Unmarshal(invoke("submit", f.writeJSON("submission.json", create)), &run) != nil || run.Status != "ready" {
		t.Fatal("accepted CLI intent did not start")
	}
	for _, command := range []string{"status", "list", "pause", "resume", "cancel"} {
		args := []string{command}
		if command != "list" {
			args = append(args, run.ID)
		}
		if command == "pause" || command == "resume" || command == "cancel" {
			args = append(args, "Synthetic operator steering")
		}
		_ = invoke(args...)
	}
	var final domain.ExecutionView
	if json.Unmarshal(invoke("status", run.ID), &final) != nil || final.Run.Status != "cancelled" {
		t.Fatal("CLI cancellation lost")
	}
}
