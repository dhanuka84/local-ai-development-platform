package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

func TestSDLCLimitsRevocationAndIngressAuditE2E(t *testing.T) {
	f := startRuntime(t)
	tokens, _ := configureExecutionFixture(t, f)
	intent, _ := executionFixtureIntent(f)
	create := func(key string) domain.ExecutionRun {
		t.Helper()
		var run domain.ExecutionRun
		f.call(f.operator, "sdlc_run_create", service.ExecutionCreateInput{ProjectID: f.project, TargetID: "synthetic-target", Kind: "feature", IntentID: intent.ID, ExpectedSHA256: intent.SHA256, IdempotencyKey: key}, true, &run)
		return run
	}
	id := func(run domain.ExecutionRun) service.ExecutionIDInput {
		return service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}
	}
	claim := func(run domain.ExecutionRun, success bool) domain.ExecutionClaim {
		t.Helper()
		var c domain.ExecutionClaim
		f.call(tokens["sdlc_builder"], "sdlc_step_claim", id(run), success, &c)
		return c
	}
	cancel := func(run domain.ExecutionRun) {
		t.Helper()
		var v domain.ExecutionView
		f.call(f.operator, "sdlc_run_get", id(run), true, &v)
		f.call(f.operator, "sdlc_run_control", service.ExecutionControlInput{ExecutionIDInput: id(run), ExpectedVersion: v.Run.Version, Action: "cancel", Reason: "Controlled negative acceptance complete"}, true, nil)
	}
	first, second := create("concurrent-first"), create("concurrent-second")
	claim(first, true)
	claim(second, false)
	cancel(first)
	c := claim(second, true)
	// Each lost worker is charged before dispatch, including exact-step retry.
	for i := 1; i < second.Target.Budget.MaxModelCalls; i++ {
		if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE sdlc_execution_steps SET lease_until=now()-interval '1 second',record=jsonb_set(record,'{lease_until}',to_jsonb(now()-interval '1 second')) WHERE id=$1`, c.Step.ID); err != nil {
			t.Fatal(err)
		}
		c = claim(second, true)
	}
	if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE sdlc_execution_steps SET lease_until=now()-interval '1 second',record=jsonb_set(record,'{lease_until}',to_jsonb(now()-interval '1 second')) WHERE id=$1`, c.Step.ID); err != nil {
		t.Fatal(err)
	}
	claim(second, false)
	var v domain.ExecutionView
	f.call(f.operator, "sdlc_run_get", id(second), true, &v)
	if v.Run.Usage.ModelCalls != 4 || v.Run.Status != "blocked" || !strings.Contains(strings.Join(v.Run.Blockers, ","), "model_budget_exhausted") {
		t.Fatal("retry bypassed total model budget")
	}
	cancel(second)
	expired := create("expired-deadline")
	if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE sdlc_executions SET record=jsonb_set(record,'{deadline}',to_jsonb(now()-interval '1 second')) WHERE id=$1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	claim(expired, false)
	cancel(expired)
	runs := []domain.ExecutionRun{}
	for i := 0; i < 4; i++ {
		runs = append(runs, create(fmt.Sprint("capacity-", i)))
	}
	f.call(f.operator, "sdlc_run_create", service.ExecutionCreateInput{ProjectID: f.project, TargetID: "synthetic-target", Kind: "feature", IntentID: intent.ID, ExpectedSHA256: intent.SHA256, IdempotencyKey: "capacity-overflow"}, false, nil)
	for _, run := range runs {
		cancel(run)
	}
	// Malformed arguments and failed pre-tool hydration must have terminal
	// evidence even though the ordinary typed handler never starts.
	_, raw := f.rpc(f.operator, "tools/call", map[string]any{"name": "sdlc_run_get", "arguments": map[string]any{"project_id": f.project, "run_id": 42, "sensitive": "synthetic-body-sentinel"}})
	if !strings.Contains(string(raw), "error") && !strings.Contains(string(raw), "isError") {
		t.Fatal("malformed request accepted")
	}
	f.call(f.operator, "knowledge_get", map[string]any{"id": "00000000-0000-0000-0000-000000000000"}, false, nil)
	status, _ := f.rpc("synthetic-invalid-credential", "tools/list", map[string]any{})
	if status != http.StatusUnauthorized {
		t.Fatalf("authentication failure: %d", status)
	}
	for _, name := range []string{"mcp.envelope.sdlc_run_get", "mcp.envelope.knowledge_get", "http.authentication"} {
		var count int
		if err := f.repo.Pool().QueryRow(f.ctx, `SELECT count(*) FROM operation_records WHERE record->>'name'=$1 AND phase='outcome' AND outcome IN ('failed','denied')`, name).Scan(&count); err != nil || count == 0 {
			t.Fatalf("missing early denial evidence %s: %v", name, err)
		}
	}
	var leaked int
	if err := f.repo.Pool().QueryRow(f.ctx, `SELECT count(*) FROM operation_records WHERE record::text LIKE '%synthetic-body-sentinel%' OR record::text LIKE '%synthetic-invalid-credential%'`).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatal("audit retained rejected sensitive bodies", err)
	}
	// Restart may synchronize configuration but must never resurrect revocation.
	revoked := create("revoked-workload")
	c = claim(revoked, true)
	if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE principal_credentials SET revoked_at=now() WHERE principal_id=$1`, revoked.Target.Participants["sdlc_builder"]); err != nil {
		t.Fatal(err)
	}
	f.stopGateway()
	f.stopGateway = f.start("gateway")
	waitExecutionGateway(f)
	status, _ = f.rpc(tokens["sdlc_builder"], "tools/call", map[string]any{"name": "sdlc_step_context", "arguments": domain.ExecutionLease{ProjectID: f.project, RunID: revoked.ID, StepID: c.Step.ID, Fence: c.Step.Fence, Token: c.Token}})
	if status != http.StatusUnauthorized {
		t.Fatal("revoked credential was resurrected by bootstrap")
	}
}
