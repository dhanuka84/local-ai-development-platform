package e2e

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/contextregistry"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
)

type taskResult struct {
	Task domain.WorkflowTaskCheckpoint `json:"task"`
}
type itemResult struct {
	Item domain.KnowledgeItem `json:"item"`
}
type searchResult struct {
	Backend string             `json:"backend"`
	Results []domain.SearchHit `json:"results"`
}

// Real binaries and network/storage/policy adapters; deterministic synthetic
// embeddings and test-only human identities. This does not certify model quality
// or grant an operating-stage, publication, retention or deployment decision.
func TestAgentReadyGatewayE2E(t *testing.T) {
	f := startRuntime(t)
	run := func(name string, test func(*testing.T)) {
		t.Helper()
		if !t.Run(name, func(child *testing.T) {
			f.t = child
			defer func() { f.t = t }()
			test(child)
		}) {
			t.FailNow()
		} // Later steps must not run with invalid prerequisite state.
	}
	var workflow domain.WorkflowRun
	var a, b domain.WorkflowTaskCheckpoint
	var candidateA, candidateB string
	var revokedTaskToken string
	var validationA domain.KnowledgeValidation
	const lesson = "Synthetic normalization lesson: collapse whitespace and casefold, then check idempotence."
	begin := func(key string) domain.WorkflowTaskCheckpoint {
		var result taskResult
		f.call(f.operator, "workflow_task_begin", map[string]any{"workflow_id": workflow.ID, "task_key": key, "title": key, "task_type": "maintenance", "rag_query": lesson, "idempotency_key": key}, true, &result)
		return result.Task
	}
	capture := func(task domain.WorkflowTaskCheckpoint) string {
		var result struct {
			ID     string `json:"candidate_id"`
			Status string `json:"status"`
		}
		f.call(f.operator, "generation_capture", map[string]any{"project_id": f.project, "workflow_id": workflow.ID, "prompt": task.Title, "response": lesson, "summary": "Synthetic normalization", "procedure": []string{"Apply bounded patch", "Run behavior tests"}, "provider": "ollama", "model": "synthetic-e2e-fixture", "repository_revision": f.revision, "task_type": "maintenance"}, true, &result)
		if result.Status != "pending" || result.ID == "" {
			f.t.Fatal("capture did not remain pending")
		}
		return result.ID
	}
	transition := func(task domain.WorkflowTaskCheckpoint, event, candidate, validation string) domain.WorkflowTaskCheckpoint {
		var result taskResult
		f.call(f.operator, "workflow_task_transition", map[string]any{"task_id": task.ID, "expected_version": task.Version, "event_type": event, "candidate_id": candidate, "provider": "ollama", "model": "synthetic-e2e-fixture", "evidence": "Synthetic E2E event", "idempotency_key": task.ID + ":" + event, "payload": map[string]any{"validation_id": validation}}, true, &result)
		return result.Task
	}
	verify := func(candidate, module, separator string) domain.KnowledgeValidation {
		input := service.ValidationInput{KnowledgeID: candidate, ExpectedVersion: 1,
			SourceManifest: domain.SourceManifest{SchemaVersion: "hybrid-ai/knowledge-source/v1", Sources: []domain.KnowledgeSource{{Kind: "repository", Reference: f.source, Revision: f.revision, Branch: "main"}}},
			Criteria:       []domain.ValidationCriterion{{Name: "behavior", Passed: true, Observation: "Execute the committed Python behavior tests"}}}
		packet := workpacket.Packet{SchemaVersion: workpacket.SchemaVersion, ID: module, Goal: "Synthetic normalization", Workspace: f.source, BaseRevision: f.revision, Mode: workpacket.ModePatch, TaskClass: workpacket.TaskMaintenance, DataClassification: workpacket.DataInternal, LocalOnly: true, AllowedFiles: []string{module + ".py"}, Rollback: []string{"Discard verifier clone"}, Checks: []workpacket.Check{{Name: "diff", Argv: []string{"git", "diff", "--cached", "--check"}, TimeoutSeconds: 10}, {Name: "behavior", Argv: []string{"python3", "-m", "unittest", "-v", "test_" + module + ".py"}, TimeoutSeconds: 20}}, Limits: workpacket.Limits{MaxChangedFiles: 1, MaxDiffLines: 20, MaxPatchBytes: 12000}}
		patch := fmt.Sprintf("diff --git a/%s.py b/%s.py\nnew file mode 100644\n--- /dev/null\n+++ b/%s.py\n@@ -0,0 +1,2 @@\n+def normalize(value):\n+    return %q.join(value.split()).casefold()\n", module, module, module, separator)
		inputPath := f.writeJSON(module+"-validation.json", input)
		packetPath := f.writeJSON(module+"-packet.json", packet)
		patchPath := f.write(module+".patch", []byte(patch))
		f.call(f.executor, "knowledge_validation_record", input, false, nil)
		result := decode[domain.KnowledgeValidation](f.t, f.cli(f.executor, true, "validate", inputPath, packetPath, patchPath))
		if result.Verdict != "pass" || result.Method != "workpacket" || result.CandidateVersion != 1 {
			f.t.Fatalf("invalid verifier receipt: %+v", result)
		}
		if f.git("status", "--porcelain") != "" {
			f.t.Fatal("verifier modified source repository")
		}
		return result
	}

	run("DefaultDeveloperAuthority", func(t *testing.T) {
		hash := sha256.Sum256([]byte(f.operator))
		principal, err := f.repo.AuthenticatePrincipal(f.ctx, hash[:])
		if err != nil {
			t.Fatal(err)
		}
		roles := principal.RolesFor(f.project)
		slices.Sort(roles)
		if principal.ID != "human:local-developer" || !principal.Human ||
			!slices.Equal(roles, []string{"development", "operations", "product_owner", "qa"}) {
			t.Fatalf("unexpected default operator authority: %+v", principal)
		}
	})
	run("TransportDiscovery", func(t *testing.T) {
		for _, token := range []string{"", "invalid-synthetic-token"} {
			status, _ := f.rpc(token, "tools/list", map[string]any{})
			if status != http.StatusUnauthorized {
				t.Fatalf("unauthenticated discovery: HTTP %d", status)
			}
		}
		status, raw := f.rpc(f.operator, "tools/list", map[string]any{})
		if status != http.StatusOK {
			t.Fatalf("tool discovery HTTP %d", status)
		}
		var listing struct {
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(raw, &listing); err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, tool := range listing.Result.Tools {
			found[tool.Name] = true
		}
		for _, name := range []string{"generation_capture", "knowledge_candidate_decide", "knowledge_validation_record", "knowledge_search", "knowledge_candidate_quality", "workflow_task_context_record", "workflow_trace_get", "context_registry_validate", "context_definition_decide", "platform_metric_query", "platform_evidence_health", "repository_graph_get"} {
			if !found[name] {
				t.Fatalf("missing public tool %s", name)
			}
		}
		f.call(f.foreign, "knowledge_search", map[string]any{"project_id": f.project, "query": lesson}, false, nil)
		var created struct {
			Workflow domain.WorkflowRun `json:"workflow"`
		}
		f.call(f.operator, "workflow_run_create", map[string]any{"project_id": f.project, "request": "Synthetic E2E workflow", "risk": "low", "data_classification": "restricted", "idempotency_key": "e2e"}, true, &created)
		workflow = created.Workflow
		a = begin("task-a")
	})
	run("TaskDelegation", func(t *testing.T) {
		path := f.root + "/delegated.token"
		delegation := decode[domain.TaskDelegation](t, f.cli(f.operator, true, "delegate-task", a.ID, "60", path))
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("task credential is not private")
		}
		token := string(raw)
		f.call(token, "workflow_task_get", map[string]any{"task_id": a.ID}, true, nil)
		f.call(token, "knowledge_candidate_decide", map[string]any{"knowledge_id": "forbidden", "expected_version": 1, "decision": "approve", "reason": "denied", "idempotency_key": "denied"}, false, nil)
		f.call(token, "workflow_run_create", map[string]any{"project_id": f.project, "request": "escape", "idempotency_key": "escape"}, false, nil)
		// Loss of the issuer's live authority must affect the next HTTP call.
		if _, err := f.repo.Pool().Exec(f.ctx, `DELETE FROM principal_role_bindings
		    WHERE principal_id='human:local-developer' AND role='development'`); err != nil {
			t.Fatal(err)
		}
		status, _ := f.rpc(token, "tools/list", map[string]any{})
		if status != http.StatusUnauthorized {
			t.Fatal("delegation survived loss of its issuer's development role")
		}
		if _, err := f.repo.Pool().Exec(f.ctx, `INSERT INTO principal_role_bindings(principal_id,project_id,role)
		    VALUES('human:local-developer','*','development')`); err != nil {
			t.Fatal(err)
		}
		f.call(token, "workflow_task_get", map[string]any{"task_id": a.ID}, true, nil)
		f.cli(f.operator, true, "revoke-task-delegation", delegation.ID)
		status, _ = f.rpc(token, "tools/list", map[string]any{})
		if status != http.StatusUnauthorized {
			t.Fatal("revoked token still authenticates")
		}
		revokedTaskToken = token
		// Move this disposable credential's expiry to test the real gateway
		// clock without waiting for an observation period.
		expiring := decode[domain.TaskDelegation](t, f.cli(f.operator, true, "delegate-task", a.ID, "60", f.root+"/expiring.token"))
		raw, err = os.ReadFile(f.root + "/expiring.token")
		if err != nil {
			t.Fatal(err)
		}
		f.call(string(raw), "workflow_task_get", map[string]any{"task_id": a.ID}, true, nil)
		if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE principal_credentials SET expires_at=now()-interval '1 second'
		    WHERE principal_id=$1`, expiring.PrincipalID); err != nil {
			t.Fatal(err)
		}
		status, _ = f.rpc(string(raw), "tools/list", map[string]any{})
		if status != http.StatusUnauthorized {
			t.Fatal("expired task credential still authenticates")
		}
	})
	run("ValidationAndPublication", func(t *testing.T) {
		candidateA = capture(a)
		f.call(f.operator, "knowledge_get", map[string]any{"id": candidateA}, false, nil)
		var pending searchResult
		f.call(f.operator, "knowledge_search", map[string]any{"project_id": f.project, "query": lesson}, true, &pending)
		if len(pending.Results) != 0 {
			t.Fatal("pending knowledge leaked into retrieval")
		}
		a = transition(a, "LOCAL_RESULT_RECORDED", candidateA, "")
		f.cli(f.operator, false, "approve", candidateA, "1", "-", "no-validation", "Synthetic missing evidence denial")
		validationA = verify(candidateA, "labels", " ")
		f.call(f.operator, "workflow_task_transition", map[string]any{"task_id": a.ID, "expected_version": a.Version, "event_type": "VALIDATION_PASSED", "evidence": "pass", "idempotency_key": "text-only"}, false, nil)
		a = transition(a, "VALIDATION_PASSED", "", validationA.ID)
		decision := service.DecisionInput{KnowledgeID: candidateA, ExpectedVersion: 1, ValidationID: validationA.ID, Decision: "approve", Reason: "Explicit synthetic user decision for E2E task A version 1 after local workpacket validation", IdempotencyKey: "test-only-approval"}
		f.call(f.executor, "knowledge_candidate_decide", decision, false, nil)
		f.call(f.controller, "knowledge_candidate_decide", decision, false, nil)
		f.call(f.developer, "knowledge_candidate_decide", decision, false, nil)
		f.cli(f.executor, false, "approve", candidateA, "1", validationA.ID, "executor-denied", "Synthetic denied approval")
		stale := decision
		stale.ExpectedVersion = 2
		f.call(f.operator, "knowledge_candidate_decide", stale, false, nil)
		f.cli(f.operator, false, "approve", candidateA, "2", validationA.ID, "stale-cli", "Synthetic stale decision")
		approved := decode[domain.KnowledgeItem](t, f.cli(f.operator, true, "approve", candidateA, "1", validationA.ID, decision.IdempotencyKey, decision.Reason))
		if approved.Status != "approved" {
			t.Fatal("CLI approval did not publish exact version")
		}
		f.call(f.operator, "knowledge_candidate_decide", decision, true, nil) // Same exact request is idempotent across transports.
		var actor, reason, validationID string
		if err := f.repo.Pool().QueryRow(f.ctx, `SELECT actor,reason,validation_id::text
		    FROM knowledge_decisions WHERE knowledge_id=$1 AND idempotency_key=$2`, candidateA, decision.IdempotencyKey).Scan(&actor, &reason, &validationID); err != nil {
			t.Fatal(err)
		}
		if actor != "human:local-developer" || reason != decision.Reason || validationID != validationA.ID {
			t.Fatal("explicit operator decision lost its attribution or validation")
		}
		a = transition(a, "LEARNING_PROMOTED", "", "")
		f.wait("worker verifies exact publication", func() bool {
			quality, err := f.repo.KnowledgeQuality(f.ctx, candidateA)
			return err == nil && quality.ProjectionVerifiedAt != nil && quality.Eligible
		})
		a = transition(a, "RAG_READBACK_VERIFIED", "", "")
		var read itemResult
		f.call(f.operator, "knowledge_get", map[string]any{"id": candidateA}, true, &read)
		if read.Item.Version != 1 || read.Item.Content != lesson {
			t.Fatal("hydration returned different knowledge")
		}
		var search searchResult
		f.call(f.operator, "knowledge_search", map[string]any{"project_id": f.project, "query": lesson}, true, &search)
		if search.Backend != "milvus" || len(search.Results) != 1 || search.Results[0].ID != candidateA {
			t.Fatalf("publication search: %+v", search)
		}
		f.call(f.foreign, "knowledge_get", map[string]any{"id": candidateA}, false, nil)
	})
	run("ReuseAndTrace", func(t *testing.T) {
		b = begin("task-b")
		if b.Route != domain.TaskRouteRAGHit {
			t.Fatal("Task B did not reuse published Task A")
		}
		use := service.RecordTaskContextInput{TaskID: b.ID, ExpectedVersion: b.Version, Contexts: []domain.UsedContext{{KnowledgeID: candidateA, Version: 1, TargetRepository: f.source, TargetBranch: "main", TargetRevision: f.revision}}, IdempotencyKey: "use"}
		f.call(f.operator, "workflow_task_context_record", use, true, nil)
		f.call(f.operator, "workflow_task_context_record", use, true, nil)
		candidateB = capture(b)
		b = transition(b, "LOCAL_RESULT_RECORDED", candidateB, "")
		validated := verify(candidateB, "keys", "-")
		b = transition(b, "VALIDATED_REUSE_COMPLETED", "", validated.ID)
		item, err := f.repo.GetKnowledge(f.ctx, candidateB, true)
		if err != nil || item.Status != "pending" || b.State != "completed" {
			t.Fatal("reuse changed publication or failed to complete", err)
		}
		var trace domain.WorkflowTrace
		f.call(f.operator, "workflow_trace_get", map[string]any{"workflow_id": workflow.ID}, true, &trace)
		if !trace.Complete || len(trace.Missing) != 0 || len(trace.Records) == 0 {
			t.Fatalf("incomplete owned trace: %+v", trace.Missing)
		}
		raw, _ := json.Marshal(trace)
		if strings.Contains(string(raw), lesson) || strings.Contains(string(raw), f.operator) {
			t.Fatal("raw content or credential leaked into telemetry")
		}
		f.call(f.foreign, "workflow_trace_get", map[string]any{"workflow_id": workflow.ID}, false, nil)
	})
	run("GovernedDefinitionsAndMetrics", func(t *testing.T) {
		query := domain.MetricRequest{ProjectID: f.project, MetricID: "validated_reuse_rate", Version: 1, Start: time.Now().UTC().Add(-time.Hour), End: time.Now().UTC().Add(time.Second)}
		f.call(f.operator, "platform_metric_query", query, false, nil)
		var registry domain.RegistryValidation
		f.call(f.operator, "context_registry_validate", map[string]any{"project_id": f.project}, true, &registry)
		definitions, digest, err := contextregistry.Load()
		if err != nil || digest != registry.RegistrySHA256 {
			t.Fatal("registry digest mismatch", err)
		}
		for _, def := range definitions {
			current, err := f.repo.ContextDefinition(f.ctx, f.project, def.ID, def.Version)
			if err != nil || current.Status != "pending" {
				t.Fatal("validation published definition", err)
			}
			decision := domain.DefinitionDecision{ProjectID: f.project, DefinitionID: def.ID, ExpectedVersion: def.Version, ExpectedSHA256: current.SHA256, ValidationID: registry.ID, Decision: "approve", Reason: "Synthetic standing task authorization for locally validated registry definitions", IdempotencyKey: "definition-" + def.ID}
			f.call(f.executor, "context_definition_decide", decision, false, nil)
			f.call(f.controller, "context_definition_decide", decision, false, nil)
			f.call(f.developer, "context_definition_decide", decision, false, nil)
			stale := decision
			stale.ExpectedSHA256 = strings.Repeat("0", 64)
			f.call(f.operator, "context_definition_decide", stale, false, nil)
			f.call(f.operator, "context_definition_decide", decision, true, nil)
			f.call(f.operator, "context_definition_decide", decision, true, nil)
			var actor, reason, validationID string
			if err := f.repo.Pool().QueryRow(f.ctx, `SELECT actor,reason,validation_id::text
			    FROM context_definition_decisions WHERE project_id=$1 AND definition_id=$2 AND idempotency_key=$3`, f.project, def.ID, decision.IdempotencyKey).Scan(&actor, &reason, &validationID); err != nil {
				t.Fatal(err)
			}
			if actor != "human:local-developer" || reason != decision.Reason || validationID != registry.ID {
				t.Fatal("delegated definition decision lost its attribution or validation")
			}
		}
		lesson, err := f.repo.GetKnowledge(f.ctx, candidateB, true)
		if err != nil || lesson.Status != "pending" {
			t.Fatal("definition publication must leave reusable lessons pending", err)
		}
		for metric, expected := range map[string]float64{"validated_reuse_rate": .5, "candidate_validation_rate": 1, "candidate_approval_rate": 1, "pending_index_age_seconds": 0} {
			query.MetricID = metric
			var result domain.MetricResult
			f.call(f.operator, "platform_metric_query", query, true, &result)
			if result.Value == nil || *result.Value != expected || result.DefinitionSHA256 == "" || result.RegistrySHA256 != digest {
				t.Fatalf("%s: %+v", metric, result)
			}
			f.call(f.foreign, "platform_metric_query", query, false, nil)
		}
		query.MetricID = "validated_reuse_rate"
		query.Dimensions = map[string]string{"task_type": "x' OR 1=1 --"}
		var empty domain.MetricResult
		f.call(f.operator, "platform_metric_query", query, true, &empty)
		if empty.Value != nil || empty.Denominator != 0 {
			t.Fatal("dimension was not a literal parameter")
		}
		f.wait("semantic definition publication", func() bool {
			for _, definition := range definitions {
				current, err := f.repo.ContextDefinition(f.ctx, f.project, definition.ID, definition.Version)
				if err != nil {
					t.Fatal("read definition publication receipt:", err)
				}
				if current.ProjectionVerifiedAt == nil {
					return false
				}
			}
			return true
		})
		var found struct {
			Definitions []domain.GovernedDefinition `json:"definitions"`
		}
		f.call(f.operator, "context_definition_search", map[string]any{"project_id": f.project, "query": "validated reuse", "limit": 10}, true, &found)
		if len(found.Definitions) != len(definitions) {
			t.Fatal("semantic search missed published definitions")
		}
		if _, err = f.repo.Pool().Exec(f.ctx, `UPDATE context_definitions SET validated_at=now()-interval '31 days' WHERE project_id=$1`, f.project); err != nil {
			t.Fatal(err)
		}
		query.Dimensions = nil
		f.call(f.operator, "platform_metric_query", query, false, nil)
		f.call(f.operator, "context_definition_search", map[string]any{"project_id": f.project, "query": "validated reuse", "limit": 10}, true, &found)
		if len(found.Definitions) != 0 {
			t.Fatal("stale definitions escaped PostgreSQL hydration")
		}
		// Fresh validation of the same approved version restores eligibility
		// while preserving ownership and the original publication decision.
		var refreshed domain.RegistryValidation
		f.call(f.developer, "context_registry_validate", map[string]any{"project_id": f.project}, false, nil)
		f.call(f.operator, "context_registry_validate", map[string]any{"project_id": f.project}, true, &refreshed)
		if refreshed.ID == registry.ID || refreshed.RegistrySHA256 != digest {
			t.Fatal("definition revalidation lost exact registry binding")
		}
		var recovered domain.MetricResult
		f.call(f.operator, "platform_metric_query", query, true, &recovered)
		if recovered.Value == nil || *recovered.Value != .5 {
			t.Fatal("fresh validation did not restore the governed metric")
		}
		for _, definition := range definitions {
			current, err := f.repo.ContextDefinition(f.ctx, f.project, definition.ID, definition.Version)
			if err != nil || current.ApprovedBy != "human:local-developer" || current.Owner != definition.Owner || current.Status != "approved" {
				t.Fatal("definition refresh changed ownership or publication", err)
			}
		}
	})
	run("QualityAndFreshness", func(t *testing.T) {
		var quality domain.CandidateQualityAssessment
		f.call(f.operator, "knowledge_candidate_quality", map[string]any{"knowledge_id": candidateB, "expected_version": 1}, true, &quality)
		if len(quality.PossibleDuplicates) != 1 || quality.PossibleDuplicates[0].KnowledgeID != candidateA {
			t.Fatal("missing authoritative duplicate candidate")
		}
		f.call(f.operator, "knowledge_candidate_quality", map[string]any{"knowledge_id": candidateB, "expected_version": 2}, false, nil)
		f.call(f.foreign, "knowledge_candidate_quality", map[string]any{"knowledge_id": candidateB, "expected_version": 1}, false, nil)
		// Advance the real source instead of fabricating a successful verifier clock.
		f.write("source/new-source.txt", []byte("Synthetic source change\n"))
		f.git("add", ".")
		f.git("-c", "user.name=Synthetic E2E", "-c", "user.email=e2e@example.invalid", "commit", "--quiet", "-m", "Advance synthetic source")
		// Worker source refresh is due on its own schedule; make only the mutable
		// scheduling timestamp due in this disposable fixture.
		if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE knowledge_sources SET verified_at=now()-interval '2 days' WHERE validation_id=$1`, validationA.ID); err != nil {
			t.Fatal(err)
		}
		f.wait("worker records actual failed source check", func() bool {
			var failed bool
			err := f.repo.Pool().QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_source_check_receipts WHERE validation_id=$1 AND NOT valid)`, validationA.ID).Scan(&failed)
			if err != nil {
				t.Fatal("read source verification receipt:", err)
			}
			return failed
		})
		f.call(f.operator, "knowledge_get", map[string]any{"id": candidateA}, false, nil)
		f.call(f.operator, "knowledge_search", map[string]any{"project_id": f.project, "query": lesson}, false, nil)
		f.call(f.operator, "knowledge_candidate_quality", map[string]any{"knowledge_id": candidateB, "expected_version": 1}, true, &quality)
		if len(quality.PossibleDuplicates) != 0 {
			t.Fatal("quarantined source returned as duplicate guidance")
		}
		stored, err := f.repo.GetKnowledge(f.ctx, candidateA, true)
		if err != nil || stored.Status != "approved" {
			t.Fatal("source quarantine rewrote historical approval", err)
		}
	})
	run("EvidenceRetentionAndExport", func(t *testing.T) {
		_, span, record := telemetry.Start(f.ctx, "synthetic.retention.fixture", domain.OperationScope{ProjectID: f.project, WorkflowID: workflow.ID}, nil)
		span.End()
		record.RecordedAt = time.Now().UTC().Add(-31 * 24 * time.Hour)
		if err := f.repo.AppendOperation(f.ctx, record); err != nil {
			t.Fatal(err)
		}
		var health domain.EvidenceHealth
		f.call(f.operator, "platform_evidence_health", map[string]any{"project_id": f.project}, true, &health)
		if health.RetentionReviewDue == 0 || health.UnknownOutcomes == 0 || health.RetentionDays != 30 {
			t.Fatalf("missing retention/unknown-outcome alerts: %+v", health)
		}
		for _, statement := range []string{`UPDATE operation_records SET outcome='success' WHERE id=$1`, `DELETE FROM operation_records WHERE id=$1`} {
			if _, err := f.repo.Pool().Exec(f.ctx, statement, record.ID); err == nil {
				t.Fatal("immutable evidence was changed")
			}
		}
		f.wait("worker export receipt from actual collector", func() bool {
			var exported bool
			err := f.repo.Pool().QueryRow(f.ctx, `SELECT exported_at IS NOT NULL FROM trace_export_queue WHERE record_id=$1`, record.ID).Scan(&exported)
			return err == nil && exported
		})
		f.call(f.operator, "platform_evidence_health", map[string]any{"project_id": f.project}, true, &health)
		if health.RetentionReviewDue == 0 {
			t.Fatal("export erased retention obligation")
		}
		f.call(f.foreign, "platform_evidence_health", map[string]any{"project_id": f.project}, false, nil)
	})
	run("RestartPreservesState", func(t *testing.T) {
		f.stopWorker()
		f.stopGateway()
		f.stopGateway = f.start("gateway")
		f.stopWorker = f.start("worker")
		f.wait("restarted gateway readiness", func() bool {
			response, err := http.Get(f.endpoint + "/readyz")
			if err != nil {
				return false
			}
			defer response.Body.Close()
			return response.StatusCode == http.StatusOK
		})
		var checkpoint taskResult
		f.call(f.operator, "workflow_task_get", map[string]any{"task_id": b.ID}, true, &checkpoint)
		if checkpoint.Task.State != "completed" || checkpoint.Task.Version != b.Version {
			t.Fatal("restart lost the completed task checkpoint")
		}
		pending, err := f.repo.GetKnowledge(f.ctx, candidateB, true)
		if err != nil || pending.Status != "pending" {
			t.Fatal("restart changed pending KB-entry publication", err)
		}
		f.call(f.operator, "knowledge_get", map[string]any{"id": candidateB}, false, nil)
		f.call(f.operator, "knowledge_get", map[string]any{"id": candidateA}, false, nil)
		status, _ := f.rpc(revokedTaskToken, "tools/list", map[string]any{})
		if status != http.StatusUnauthorized {
			t.Fatal("restart restored a revoked task credential")
		}
		f.call(f.foreign, "workflow_task_get", map[string]any{"task_id": b.ID}, false, nil)
		var health domain.EvidenceHealth
		f.call(f.operator, "platform_evidence_health", map[string]any{"project_id": f.project}, true, &health)
		if health.RetentionReviewDue == 0 || health.UnknownOutcomes == 0 {
			t.Fatal("restart lost durable evidence alerts")
		}
		_, span, record := telemetry.Start(f.ctx, "synthetic.after_restart", domain.OperationScope{ProjectID: f.project, WorkflowID: workflow.ID}, nil)
		span.End()
		if err := f.repo.AppendOperation(f.ctx, record); err != nil {
			t.Fatal(err)
		}
		f.wait("restarted worker exports new evidence", func() bool {
			var exported bool
			err := f.repo.Pool().QueryRow(f.ctx, `SELECT exported_at IS NOT NULL FROM trace_export_queue WHERE record_id=$1`, record.ID).Scan(&exported)
			return err == nil && exported
		})
	})
}
