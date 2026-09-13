package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

type pilotReport struct {
	Status       string                `json:"status"`
	WorkflowID   string                `json:"workflow_id"`
	TaskAID      string                `json:"task_a_id"`
	TaskBID      string                `json:"task_b_id"`
	KnowledgeID  string                `json:"knowledge_id"`
	Version      int                   `json:"version"`
	ValidationID string                `json:"validation_id"`
	Trace        *domain.WorkflowTrace `json:"trace"`
}

// A deterministic protocol fixture drives the actual pilot executable through
// failed generation verification, rejected repair, recount and local revision.
// This tests orchestration, not the quality of any real model's generation.
func TestAgentReadyPilotRecoveryE2E(t *testing.T) {
	f := startRuntime(t)
	f.env["AGENT_READY_PILOT_ISOLATED"] = "true"
	const model = "synthetic-e2e-fixture"
	const lesson = "Synthetic reusable normalization: canonicalize whitespace and case, and verify idempotence."
	packet := func(module string) workpacket.Packet {
		return workpacket.Packet{SchemaVersion: workpacket.SchemaVersion, ID: module, Goal: "Add " + module + ".py normalization", Workspace: f.source, BaseRevision: f.revision, Mode: workpacket.ModePatch, TaskClass: workpacket.TaskMaintenance, DataClassification: workpacket.DataInternal, LocalOnly: true, AllowedFiles: []string{module + ".py"}, Rollback: []string{"Discard disposable verifier clone"}, Checks: []workpacket.Check{{Name: "diff", Argv: []string{"git", "diff", "--cached", "--check"}, TimeoutSeconds: 10}, {Name: "behavior", Argv: []string{"python3", "-m", "unittest", "-v", "test_" + module + ".py"}, TimeoutSeconds: 20}}, Limits: workpacket.Limits{MaxChangedFiles: 1, MaxDiffLines: 20, MaxPatchBytes: 12000}}
	}
	patch := func(module, separator string) string {
		return fmt.Sprintf("diff --git a/%s.py b/%s.py\nnew file mode 100644\n--- /dev/null\n+++ b/%s.py\n@@ -0,0 +1,2 @@\n+def normalize(value):\n+    return %q.join(value.split()).casefold()\n", module, module, module, separator)
	}
	envelope := func(diff, summary string) []byte {
		answer, err := json.Marshal(map[string]string{"patch": diff, "summary": summary, "lesson": lesson})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(map[string]any{"model": model, "done": true, "response": string(answer)})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	spec := map[string]any{"project_id": f.project, "run_key": "recovery-e2e", "model": model, "branch": "main", "task_a": packet("labels"), "task_b": packet("keys")}
	invoke := func(success bool) []byte {
		return f.invoke("agent-ready-pilot", f.executor, success, f.writeJSON("pilot.json", spec))
	}
	getTask := func(workflow, key string) domain.WorkflowTaskCheckpoint {
		var id string
		if err := f.repo.Pool().QueryRow(f.ctx, `SELECT id::text FROM workflow_task_checkpoints WHERE workflow_id=$1 AND task_key=$2`, workflow, key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		task, err := f.repo.GetWorkflowTask(f.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	traceFor := func(workflow string) domain.WorkflowTrace {
		var trace domain.WorkflowTrace
		f.call(f.operator, "workflow_trace_get", map[string]any{"workflow_id": workflow}, true, &trace)
		return trace
	}
	boundRepair := func(task domain.WorkflowTaskCheckpoint, raw []byte) map[string]any {
		trace := traceFor(task.WorkflowID)
		var verifier string
		for _, record := range trace.Records {
			if record.TaskID == task.ID && record.Name == "local.verifier.result" && record.Outcome == "success" {
				for _, reference := range record.References {
					if reference.Kind == "artifact" {
						verifier = reference.SHA256
					}
				}
			}
		}
		if verifier == "" {
			t.Fatal("failed verifier output was not retained")
		}
		store := artifacts.NewLocalStore(f.env["ARTIFACTS_PATH"])
		exact, err := store.Read(f.ctx, domain.Digest(raw))
		if err != nil || string(exact) != string(raw) {
			t.Fatal("exact generation envelope changed", err)
		}
		return map[string]any{"expected_task_version": task.Version, "knowledge_id": task.CandidateID, "expected_version": 1, "model_output_sha256": domain.Digest(raw), "verifier_result_sha256": verifier}
	}

	t.Log("Malformed Task A output must fail without losing its evidence")
	malformed := envelope(strings.Replace(patch("labels", " "), "+1,2 @@", "+1,3 @@", 1), "Synthetic labels normalization")
	f.generationResponses <- malformed
	invoke(false)
	var workflow string
	if err := f.repo.Pool().QueryRow(f.ctx, `SELECT id::text FROM workflow_runs WHERE project_id=$1`, f.project).Scan(&workflow); err != nil {
		t.Fatal(err)
	}
	a := getTask(workflow, "recovery-e2e:a")
	if a.State != domain.TaskStateValidationRequired {
		t.Fatalf("Task A state=%s", a.State)
	}
	spec["workflow_id"], spec["task_a_id"] = workflow, a.ID
	repair := boundRepair(a, malformed)
	spec["repair_task_a"] = repair

	t.Log("Unchanged local-model repair must fail; recount must preserve source and lesson")
	f.generationResponses <- malformed
	invoke(false)
	trace := traceFor(workflow)
	failedRepair := false
	for _, record := range trace.Records {
		if record.Name == "pilot.patch_repair" && record.Phase == "outcome" && record.Outcome == "failed" {
			failedRepair = true
		}
	}
	if !failedRepair {
		t.Fatal("unchanged repair has no durable failed outcome")
	}
	repair["method"] = "recount"
	first := decode[pilotReport](t, invoke(true))
	if first.Status != "awaiting_human_approval" || first.ValidationID == "" || first.Version != 1 {
		t.Fatalf("repair did not stop at approval: %+v", first)
	}
	item, err := f.repo.GetKnowledge(f.ctx, a.CandidateID, true)
	if err != nil || item.Status != "pending" || item.Content != lesson {
		t.Fatal("repair changed or published lesson", err)
	}
	// Reinvocation at the human gate must remain a no-op, without generation.
	again := decode[pilotReport](t, invoke(true))
	if again.Status != "awaiting_human_approval" {
		t.Fatal("runner bypassed human gate")
	}

	t.Log("Only a synthetic operator decision may publish the test fixture")
	decision := service.DecisionInput{KnowledgeID: item.ID, ExpectedVersion: 1, ValidationID: first.ValidationID, Decision: "approve", Reason: "Synthetic recovery test only", IdempotencyKey: "pilot-fixture-approval"}
	f.call(f.executor, "knowledge_candidate_decide", decision, false, nil)
	f.call(f.operator, "knowledge_candidate_decide", decision, true, nil)
	a = getTask(workflow, "recovery-e2e:a")
	f.call(f.operator, "workflow_task_transition", map[string]any{"task_id": a.ID, "expected_version": a.Version, "event_type": "LEARNING_PROMOTED", "provider": "ollama", "model": model, "evidence": "Synthetic explicit approval", "idempotency_key": "synthetic-promoted"}, true, nil)
	f.wait("pilot worker exact read-back", func() bool {
		quality, err := f.repo.KnowledgeQuality(f.ctx, item.ID)
		return err == nil && quality.ProjectionVerifiedAt != nil
	})
	delete(spec, "repair_task_a")

	t.Log("Task B must retain a wrong-file failure and reject stale revision bindings")
	wrongTask := envelope(patch("labels", " "), "Synthetic wrong task")
	f.generationResponses <- wrongTask
	invoke(false)
	b := getTask(workflow, "recovery-e2e:b")
	if b.State != domain.TaskStateValidationRequired {
		t.Fatalf("Task B state=%s", b.State)
	}
	revise := boundRepair(b, wrongTask)
	spec["revise_task_b"] = revise
	revise["expected_version"] = 2
	invoke(false)
	revise["expected_version"] = 1
	corrected := envelope(patch("keys", "-"), "Synthetic keys normalization")
	f.generationResponses <- corrected
	final := decode[pilotReport](t, invoke(true))
	if final.Status != "completed" || final.Trace == nil || !final.Trace.Complete || len(final.Trace.Missing) != 0 {
		t.Fatalf("incomplete recovered pilot: %+v", final)
	}
	itemB, err := f.repo.GetKnowledge(f.ctx, b.CandidateID, true)
	if err != nil || itemB.Status != "pending" || itemB.Version != 2 || itemB.Summary != "Synthetic keys normalization" {
		t.Fatal("revised Task B publication/version is wrong", err)
	}
	uses, err := f.repo.TaskUsedContexts(f.ctx, b.ID)
	if err != nil || len(uses) != 1 || uses[0].KnowledgeID != a.CandidateID || uses[0].Version != 1 {
		t.Fatal("missing exact Task A reuse receipt", err)
	}
	if f.git("status", "--porcelain") != "" || len(f.generationResponses) != 0 {
		t.Fatal("source modified or planned generation did not run")
	}
	store := artifacts.NewLocalStore(f.env["ARTIFACTS_PATH"])
	for _, expected := range [][]byte{malformed, wrongTask, corrected} {
		actual, err := store.Read(f.ctx, domain.Digest(expected))
		if err != nil || string(actual) != string(expected) {
			t.Fatal("recovery lost exact output", err)
		}
	}
	foundManifest, foundDerived := false, false
	for _, record := range final.Trace.Records {
		if record.Name == "pilot.patch_repair.derived" {
			foundDerived = true
		}
		if record.Name != "model.generate.input" {
			continue
		}
		for _, reference := range record.References {
			if reference.Kind != "artifact" {
				continue
			}
			raw, err := store.Read(f.ctx, reference.SHA256)
			if err != nil {
				t.Fatal(err)
			}
			var manifest map[string]any
			if json.Unmarshal(raw, &manifest) == nil && manifest["schema"] == "hybrid-ai/disclosed-context/v1" && manifest["model"] == model {
				foundManifest = true
			}
		}
	}
	if !foundManifest || !foundDerived {
		t.Fatal("missing disclosure or derived-patch evidence")
	}
}
