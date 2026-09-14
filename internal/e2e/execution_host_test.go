//go:build sdlc_host

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
	"github.com/dhanuka84/hybrid-ai-platform/internal/sdlcworker"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

// Only the trusted evaluator worker talks to the host's Docker daemon. The
// patched code runs in a separate container with no socket or credentials.
func TestSDLCIndependentBuilderEvaluatorE2E(t *testing.T) {
	if os.Getenv("TEST_SDLC_SANDBOX_IMAGE") == "" {
		t.Fatal("host acceptance requires a locally built pinned sandbox image")
	}
	f := startRuntime(t)
	tokens, registry := configureExecutionFixture(t, f)
	forge := setupForgeFixture(t, f)
	registry.Targets[0].DeliverySHA256 = forge.configs["sdlc_delivery"].Digest()
	f.stopGateway()
	f.env["SDLC_RUNTIME_REGISTRY"] = f.writeJSON("execution-registry.json", registry)
	f.stopGateway = f.start("gateway")
	waitExecutionGateway(f)
	intent, _ := executionFixtureIntent(f)
	var run domain.ExecutionRun
	f.call(f.operator, "sdlc_run_create", service.ExecutionCreateInput{ProjectID: f.project, TargetID: "synthetic-target", Kind: "feature", IntentID: intent.ID, ExpectedSHA256: intent.SHA256, IdempotencyKey: "independent-feature"}, true, &run)
	defer retainExecutionFixture(t, f, run.ID)
	invokeWorker := func(role string) domain.ExecutionRun {
		cfg := sdlcworker.Config{ProjectID: f.project, MCPURL: f.endpoint + "/mcp", TokenFile: f.write(role+".token", []byte(tokens[role])), Role: role, OllamaURL: f.env["OLLAMA_URL"], Workspaces: map[string]string{"synthetic-repository": f.source}, SpoolDirectory: filepath.Join(f.root, "evidence-"+role), TrialKind: "fixture", Delivery: forge.configs[role]}
		path := f.writeJSON(role+".json", cfg)
		// Separate process environments contain only this role's credential file;
		// the operator and database secrets are not inherited by model workers.
		cmd := f.command("sdlc-worker", "", "--config", path, "--run", run.ID)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
		raw, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s worker failed: %v: %s", role, err, raw)
		}
		var out domain.ExecutionRun
		if err = json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("worker result: %v: %s", err, raw)
		}
		return out
	}
	proposal := func(body string) {
		patch := "diff --git a/labels.py b/labels.py\nnew file mode 100644\n--- /dev/null\n+++ b/labels.py\n@@ -0,0 +1,2 @@\n+def normalize(value):\n+    " + body + "\n"
		out := execution.BuilderProposal{Schema: "hybrid-ai/builder-proposal/v1", BaseRevision: f.revision, Criteria: []string{"label-behavior"}, Plan: []domain.ExecutionPlanStep{{Description: "Implement normalization while preserving independent product tests.", Criteria: []string{"label-behavior"}}}, Patch: patch}
		raw, _ := json.Marshal(out)
		response, _ := json.Marshal(map[string]any{"model": "synthetic-e2e-fixture", "response": string(raw), "done": true, "done_reason": "stop", "prompt_eval_count": 500, "eval_count": 200})
		f.generationResponses <- response
	}
	proposal("return value.lower()")
	run = invokeWorker("sdlc_builder")
	if run.Stage != "evaluate" || run.Status != "ready" {
		t.Fatalf("builder did not submit candidate: %+v", run.Blockers)
	}
	f.call(tokens["sdlc_builder"], "sdlc_step_claim", service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}, false, nil)
	run = invokeWorker("sdlc_evaluator")
	if run.Stage != "build" || run.Status != "ready" {
		t.Fatalf("incorrect patch did not enter bounded repair: stage=%s status=%s blockers=%v", run.Stage, run.Status, run.Blockers)
	}
	proposal(`return " ".join(value.split()).casefold()`)
	run = invokeWorker("sdlc_builder")
	if run.Stage != "evaluate" {
		t.Fatal("repaired proposal missing", run.Blockers)
	}
	run = invokeWorker("sdlc_evaluator")
	if run.Stage != "deliver" || run.Status != "ready" {
		t.Fatalf("valid patch did not pass independent product tests: stage=%s status=%s blockers=%v", run.Stage, run.Status, run.Blockers)
	}
	var view domain.ExecutionView
	f.call(f.operator, "sdlc_run_get", service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}, true, &view)
	if len(view.Steps) != 4 || view.Run.Usage.ModelCalls != 2 || view.Run.Usage.TokensActual != 1400 {
		t.Fatal("repair steps or actual model usage were not retained")
	}
	if view.Steps[0].Actor == view.Steps[1].Actor || view.Steps[1].Result.Outcome != "failed" || view.Steps[3].Result.Outcome != "succeeded" {
		t.Fatal("independent failure and success attribution lost")
	}
	if view.Steps[0].Result.Patch.SHA256 == view.Steps[2].Result.Patch.SHA256 {
		t.Fatal("repair did not change the candidate")
	}
	if f.git("rev-parse", "HEAD") != f.revision || strings.TrimSpace(f.git("status", "--porcelain")) != "" {
		t.Fatal("model or evaluator changed the protected source checkout")
	}
	// Simulate the real crash window: external PR exists, no durable completion
	// acknowledgement exists. Restart must reuse the branch, PR and action ID.
	role := "sdlc_delivery"
	cfg := sdlcworker.Config{ProjectID: f.project, MCPURL: f.endpoint + "/mcp", TokenFile: f.write(role+".token", []byte(tokens[role])), Role: role, Workspaces: map[string]string{"synthetic-repository": f.source}, SpoolDirectory: filepath.Join(f.root, "evidence-"+role), TrialKind: "fixture", Delivery: forge.configs[role]}
	cmd := f.command("sdlc-worker", "", "--config", f.writeJSON(role+".json", cfg), "--run", run.ID)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-forge.written:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("delivery did not reach the native PR crash window")
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	close(forge.released)
	restoreExecutionFixture(t, f)
	f.call(f.operator, "sdlc_run_get", service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}, true, &view)
	interrupted := view.Steps[len(view.Steps)-1]
	if interrupted.Stage != "deliver" || interrupted.CompletedAt != nil {
		t.Fatal("interrupted effect was incorrectly acknowledged")
	}
	if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE sdlc_execution_steps SET lease_until=now()-interval '1 second',record=jsonb_set(record,'{lease_until}',to_jsonb(now()-interval '1 second')) WHERE id=$1`, interrupted.ID); err != nil {
		t.Fatal(err)
	}
	run = invokeWorker(role)
	if run.Status != "ready" || run.Stage != "verify_delivery" {
		t.Fatalf("delivery reconciliation failed: %+v", run.Blockers)
	}
	run = invokeWorker("sdlc_evaluator")
	if run.Status != "completed" {
		t.Fatalf("independent delivery checks failed: %+v", run.Blockers)
	}
	f.call(f.operator, "sdlc_run_get", service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}, true, &view)
	if len(view.Steps) != 6 || view.Steps[4].ID != interrupted.ID || view.Steps[4].Fence <= interrupted.Fence || len(view.Steps[5].Result.Effects) != 6 {
		t.Fatal("delivery lost action identity, fencing or independent evidence")
	}
	var improvements, again service.ExecutionImprovements
	f.call(tokens["sdlc_builder"], "sdlc_improvements_propose", service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}, false, nil)
	f.call(f.operator, "sdlc_improvements_propose", service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}, true, &improvements)
	f.call(f.operator, "sdlc_improvements_propose", service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}, true, &again)
	if improvements.Knowledge.Status != "pending" || len(improvements.Proposals) != 4 || improvements.Knowledge.ID != again.Knowledge.ID {
		t.Fatal("pending improvement idempotence failed")
	}
	for _, proposal := range improvements.Proposals {
		if proposal.Status != "pending" {
			t.Fatal("improvement was published")
		}
	}
	op := f.write("operator.token", []byte(f.operator))
	output := filepath.Join(os.Getenv("AGENT_READY_REPORT_DIR"), t.Name(), "cli-evidence")
	cli := f.command("sdlc", "", "--project", f.project, "--token-file", op, "--url", f.endpoint+"/mcp", "evidence", run.ID, output)
	cli.Env = []string{"PATH=" + os.Getenv("PATH")}
	if raw, err := cli.CombinedOutput(); err != nil {
		t.Fatalf("operator evidence export: %v %s", err, raw)
	}
	if _, err := os.Stat(filepath.Join(output, view.Steps[0].Result.Model.Response.SHA256)); err != nil {
		t.Fatal("CLI omitted exact model output", err)
	}
	firstRun := run
	firstPackage := view.Run.Packages["sdlc_builder"]
	firstCampaign := ""
	reload := func() {
		f.stopGateway()
		f.env["SDLC_RUNTIME_REGISTRY"] = f.writeJSON("execution-registry.json", registry)
		f.stopGateway = f.start("gateway")
		waitExecutionGateway(f)
	}
	registry.Targets[0].Qualification = false
	reload()
	submit := func(key string, success bool) {
		t.Helper()
		f.call(f.operator, "sdlc_run_create", service.ExecutionCreateInput{ProjectID: f.project, TargetID: "synthetic-target", Kind: "feature", IntentID: intent.ID, ExpectedSHA256: intent.SHA256, IdempotencyKey: key}, success, &run)
	}
	submit("activation-gated", false)
	for _, role := range []string{"sdlc_builder", "sdlc_evaluator", "sdlc_delivery"} {
		pkg := view.Run.Packages[role]
		var campaign domain.PackageEvaluation
		input := service.PackageEvaluateInput{ProjectID: f.project, PackageID: pkg.ID, ExpectedSHA256: pkg.Digest(), RunIDs: []string{run.ID}}
		f.call(tokens[role], "sdlc_package_evaluate", input, false, nil)
		f.call(f.operator, "sdlc_package_evaluate", input, true, &campaign)
		if campaign.Outcome != "passed" || campaign.Positive == 0 || campaign.Negative == 0 {
			t.Fatal("regression campaign did not evaluate real outcomes")
		}
		var activation domain.PackageActivation
		activate := service.PackageActivateInput{ProjectID: f.project, TargetID: registry.Targets[0].ID, PackageID: pkg.ID, EvaluationID: campaign.ID, Action: "activate", Reason: "Explicit synthetic qualification decision under test operator authority"}
		f.call(f.operator, "sdlc_package_activate", activate, true, &activation)
		f.call(f.operator, "sdlc_package_activate", activate, false, nil)
		if role == "sdlc_builder" {
			firstCampaign = campaign.ID
		}
	}
	submit("activation-gated", true)
	f.call(f.operator, "sdlc_run_control", service.ExecutionControlInput{ExecutionIDInput: service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}, ExpectedVersion: run.Version, Action: "cancel", Reason: "Active package dispatch gate proved"}, true, nil)
	// Qualify a distinct package version on an isolated canary, then activate
	// and roll back by exact digest. A fixture campaign is explicitly labelled.
	v2 := firstPackage
	v2.ID += "-v2"
	v2.Version = 2
	v2.Instructions += " Preserve exact independent evaluation evidence."
	v2.RegressionID += "-v2"
	registry.Packages = append(registry.Packages, v2)
	registry.Targets[0].Packages["sdlc_builder"] = v2.ID
	registry.Targets[0].Qualification = true
	for role, cfg := range forge.configs {
		cfg.ArtifactRoot += "-v2"
		cfg.StagingRoot += "-v2"
		forge.configs[role] = cfg
	}
	registry.Targets[0].DeliverySHA256 = forge.configs["sdlc_delivery"].Digest()
	reload()
	submit("package-v2-canary", true)
	defer retainExecutionFixture(t, f, run.ID)
	proposal("return value.lower()")
	run = invokeWorker("sdlc_builder")
	run = invokeWorker("sdlc_evaluator")
	if run.Stage != "build" {
		t.Fatal("v2 canary failed to reject negative case")
	}
	proposal(`return " ".join(value.split()).casefold()`)
	run = invokeWorker("sdlc_builder")
	run = invokeWorker("sdlc_evaluator")
	run = invokeWorker("sdlc_delivery")
	run = invokeWorker("sdlc_evaluator")
	if run.Status != "completed" {
		t.Fatal("v2 canary failed", run.Blockers)
	}
	var campaign domain.PackageEvaluation
	f.call(f.operator, "sdlc_package_evaluate", service.PackageEvaluateInput{ProjectID: f.project, PackageID: v2.ID, ExpectedSHA256: v2.Digest(), RunIDs: []string{run.ID}}, true, &campaign)
	var active domain.PackageActivation
	f.call(f.operator, "sdlc_package_activate", service.PackageActivateInput{ProjectID: f.project, TargetID: "synthetic-target", PackageID: v2.ID, EvaluationID: campaign.ID, ExpectedActiveSHA256: firstPackage.Digest(), Action: "activate", Reason: "Explicit synthetic canary rollout"}, true, &active)
	registry.Targets[0].Packages["sdlc_builder"] = firstPackage.ID
	registry.Targets[0].Qualification = false
	reload()
	submit("rollback-gated", false)
	f.call(f.operator, "sdlc_package_activate", service.PackageActivateInput{ProjectID: f.project, TargetID: "synthetic-target", PackageID: firstPackage.ID, EvaluationID: firstCampaign, ExpectedActiveSHA256: v2.Digest(), Action: "rollback", Reason: "Explicit rollback to prior regression-qualified synthetic package"}, true, &active)
	if active.Version != 3 || active.PackageSHA256 != firstPackage.Digest() {
		t.Fatal("package rollback lost exact prior version")
	}
	submit("rollback-gated", true)
	proposal(`return " ".join(value.split()).casefold()`)
	run = invokeWorker("sdlc_builder")
	run = invokeWorker("sdlc_evaluator")
	var interruptedClaim domain.ExecutionClaim
	id := service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}
	f.call(tokens["sdlc_delivery"], "sdlc_step_claim", id, true, &interruptedClaim)
	lease := domain.ExecutionLease{ProjectID: f.project, RunID: run.ID, StepID: interruptedClaim.Step.ID, Fence: interruptedClaim.Step.Fence, Token: interruptedClaim.Token}
	f.call(tokens["sdlc_delivery"], "sdlc_step_context", lease, true, nil)
	if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE sdlc_executions SET record=jsonb_set(record,'{deadline}',to_jsonb(now()-interval '1 second')) WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	f.call(tokens["sdlc_delivery"], "sdlc_step_context", lease, false, nil)
	f.call(f.operator, "sdlc_run_get", id, true, &view)
	control := service.ExecutionControlInput{ExecutionIDInput: id, ExpectedVersion: view.Run.Version, Action: "reconcile", Reason: "Explicit operator grant for read-back of interrupted delivery after deadline"}
	f.call(tokens["sdlc_delivery"], "sdlc_run_control", control, false, nil)
	f.call(f.operator, "sdlc_run_control", control, true, &run)
	run = invokeWorker("sdlc_delivery")
	if run.Status != "blocked" {
		t.Fatal("absent effects were credited as reconciled delivery")
	}
	retainExecutionFixture(t, f, run.ID)
	run = firstRun
	var published int
	if err := f.repo.Pool().QueryRow(f.ctx, `SELECT count(*) FROM knowledge_items WHERE project_id=$1 AND status='approved'`, f.project).Scan(&published); err != nil || published != 0 {
		t.Fatal("execution published generated knowledge", err)
	}
}

func retainExecutionFixture(t *testing.T, f *runtime, id string) {
	t.Helper()
	view, err := f.repo.GetExecution(f.ctx, f.project, id)
	if err != nil {
		t.Error("retain execution receipt", err)
		return
	}
	root := filepath.Join(os.Getenv("AGENT_READY_REPORT_DIR"), t.Name(), id)
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Error(err)
		return
	}
	raw, _ := json.MarshalIndent(view, "", "  ")
	if err = os.WriteFile(filepath.Join(root, "execution.json"), raw, 0400); err != nil {
		t.Error(err)
	}
	store := artifacts.NewLocalStore(f.env["ARTIFACTS_PATH"])
	refs := []domain.Artifact{}
	for _, step := range view.Steps {
		refs = append(refs, step.Evidence, domain.Artifact{SHA256: step.ContextSHA})
		if step.Result == nil {
			continue
		}
		result := step.Result
		refs = append(refs, result.Patch)
		refs = append(refs, result.Artifacts...)
		for _, check := range result.Checks {
			refs = append(refs, check.Output)
		}
		for _, effect := range result.Effects {
			refs = append(refs, effect.Evidence)
		}
		if result.Model != nil {
			refs = append(refs, result.Model.Prompt, result.Model.Response, result.Model.Manifest)
		}
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if ref.SHA256 == "" || seen[ref.SHA256] {
			continue
		}
		seen[ref.SHA256] = true
		raw, err := store.Read(f.ctx, ref.SHA256)
		if err != nil {
			t.Error("missing fixture evidence", err)
			continue
		}
		if err = os.WriteFile(filepath.Join(root, ref.SHA256), raw, 0400); err != nil {
			t.Error(err)
		}
	}
}
