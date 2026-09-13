package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

// This exercises real MCP, PostgreSQL, Cerbos, Milvus and worker boundaries.
// All BRS, approvals and source events are synthetic disposable fixtures.
func TestProductKnowledgeE2E(t *testing.T) {
	f := startRuntime(t)
	put := func(key, kind, content string, version int) domain.ProductRecord {
		var record domain.ProductRecord
		f.call(f.operator, "product_record_put", domain.ProductRecordInput{ProjectID: f.project, ProductID: "checkout", Key: key, Kind: kind, ExpectedVersion: version, Title: key, Content: content, Classification: "internal", SourceID: "synthetic-brs", SourceRevision: f.revision}, true, &record)
		if record.Status != "pending" || record.SHA256 != record.Digest() {
			t.Fatal("proposal did not preserve pending exact version")
		}
		return record
	}
	validate := func(record domain.ProductRecord) domain.ProductValidation {
		var v domain.ProductValidation
		input := service.ValidateProductInput{ProjectID: f.project, RecordID: record.ID, ExpectedSHA256: record.SHA256, Evidence: []string{"Synthetic QA attestation: source and acceptance meaning match controlled fixture"}}
		f.call(f.developer, "product_record_validate", input, false, nil)
		f.call(f.operator, "product_record_validate", input, true, &v)
		if v.Method != "human_attestation" {
			t.Fatal("manual evidence represented as command execution")
		}
		return v
	}
	accept := func(record domain.ProductRecord) domain.ProductRecord {
		v := validate(record)
		decision := domain.ProductDecision{ProjectID: f.project, RecordID: record.ID, ExpectedSHA256: record.SHA256, Decision: "accept", Reason: "Explicit synthetic fixture approval", ValidationID: v.ID, IdempotencyKey: record.ID}
		f.call(f.developer, "product_record_decide", decision, false, nil)
		var out domain.ProductRecord
		f.call(f.operator, "product_record_decide", decision, true, &out)
		f.call(f.operator, "product_record_decide", decision, true, &out)
		if out.Status != "accepted" {
			t.Fatal("validated explicit fixture decision did not publish")
		}
		return out
	}
	brs := put("BRS-ORDER", "brs", "Every accepted order must reach processed state in the audit data.", 0)
	f.call(f.operator, "product_record_get", map[string]any{"project_id": f.project, "record_id": brs.ID, "current_only": true}, false, nil)
	f.call(f.operator, "product_record_decide", domain.ProductDecision{ProjectID: f.project, RecordID: brs.ID, ExpectedSHA256: brs.SHA256, Decision: "accept", Reason: "Missing validation must fail", IdempotencyKey: "invalid"}, false, nil)
	brs = accept(brs)
	feature := accept(put("FEATURE-ORDER", "feature", "Order processing implements BRS-ORDER with verifiable outcomes.", 0))
	var relation domain.ProductRelation
	f.call(f.operator, "product_relation_put", domain.ProductRelation{ProjectID: f.project, ProductID: "checkout", FromID: feature.ID, ToID: brs.ID, Kind: "implements", Evidence: "Synthetic accepted requirement-to-feature mapping"}, true, &relation)
	if relation.Actor != "human:local-developer" {
		t.Fatal("relationship actor not authenticated")
	}
	f.wait("product projection", func() bool { _, err := f.repo.ProductProjection(f.ctx, brs.ID); return err == nil })
	var context domain.ProductContext
	query := domain.ProductContextRequest{ProjectID: f.project, ProductID: "checkout", Query: "accepted order processed", RootIDs: []string{feature.ID}, Limit: 10, MaxBytes: 16000}
	f.call(f.operator, "product_context_search", query, true, &context)
	if context.Backend != "milvus+postgres-structural" || len(context.Records) < 2 || len(context.Relations) != 1 {
		t.Fatalf("shared context incomplete: %+v", context)
	}
	f.call(f.foreign, "product_context_search", query, false, nil)
	query.MaxBytes = 256
	f.call(f.operator, "product_context_search", query, true, &context)
	if !context.Truncated || context.Bytes > 256 {
		t.Fatal("context budget not enforced")
	}
	query.MaxBytes = 16000
	newBRS := put("BRS-ORDER", "brs", "Every accepted order must reach processed state within the updated criterion.", 1)
	var current domain.ProductRecord
	f.call(f.operator, "product_record_get", map[string]any{"project_id": f.project, "record_id": brs.ID, "current_only": true}, true, &current)
	newBRS = accept(newBRS)
	f.call(f.operator, "product_record_get", map[string]any{"project_id": f.project, "record_id": brs.ID, "current_only": true}, false, nil)
	query.Query = ""
	query.RootIDs = []string{feature.ID, newBRS.ID}
	f.call(f.operator, "product_context_search", query, true, &context)
	if len(context.Relations) != 0 {
		t.Fatal("stale exact-version relationship survived requirement change")
	}
	if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE product_records SET record=record || '{"content":"tampered"}' WHERE id=$1`, newBRS.ID); err == nil {
		t.Fatal("immutable product version modified")
	}
	if _, err := f.repo.Pool().Exec(f.ctx, `DELETE FROM product_record_decisions WHERE record_id=$1`, newBRS.ID); err == nil {
		t.Fatal("publication decision removed")
	}
	f.stopGateway()
	f.stopGateway = f.start("gateway")
	f.wait("gateway restart", func() bool {
		r, e := http.Get(f.endpoint + "/readyz")
		if e != nil {
			return false
		}
		defer r.Body.Close()
		return r.StatusCode == 200
	})
	f.call(f.operator, "product_record_get", map[string]any{"project_id": f.project, "record_id": newBRS.ID, "current_only": true}, true, &current)
	if current.SHA256 != newBRS.SHA256 {
		t.Fatal("accepted product context lost on restart")
	}
}

func TestEvaluatedSourceIngestionE2E(t *testing.T) {
	f := startRuntime(t)
	var calls atomic.Int32
	var invalid atomic.Bool
	var processed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var q domain.SourceQuery
		if json.NewDecoder(r.Body).Decode(&q) != nil {
			http.Error(w, "bad fixture request", 400)
			return
		}
		timestamp, _ := json.Marshal(q.Start.Add(time.Second))
		rows := []map[string]json.RawMessage{{"event_id": json.RawMessage(`"order-1"`), "event_time": timestamp, "status": json.RawMessage(`"accepted"`)}}
		if processed.Load() && (q.SourceID == "audit" || q.SourceID == "lake") {
			rows[0]["status"] = json.RawMessage(`"processed"`)
		}
		for _, field := range q.Fields {
			if field == "internal_cost" {
				rows[0][field] = json.RawMessage(`12.5`)
			}
		}
		if invalid.Load() {
			rows[0]["unapproved_personal_field"] = json.RawMessage(`"synthetic forbidden field"`)
		}
		watermark := q.End
		if q.SourceID == "lake" {
			watermark = q.Start
		}
		_ = json.NewEncoder(w).Encode(domain.SourceEnvelope{SchemaVersion: "orders/v1", Revision: "synthetic-source-v1", Start: q.Start, End: q.End, Watermark: watermark, Complete: true, Snapshot: "snapshot-1", Offsets: map[string]int64{"orders-0": 7}, Rows: rows})
	}))
	defer server.Close()
	descriptors := []domain.SourceDescriptor{}
	for _, kind := range []string{"log", "metric", "event", "lake", "audit"} {
		descriptors = append(descriptors, domain.SourceDescriptor{ID: kind, ProjectID: f.project, ProductID: "checkout", Kind: kind, Endpoint: server.URL, AllowLoopbackHTTP: true, SchemaVersion: "orders/v1", Classification: "internal", Owner: "human:local-developer", Roles: []string{"operations", "incident_diagnosis"}, Purposes: []string{"diagnosis"}, Fields: map[string]string{"event_id": "string", "event_time": "timestamp", "status": "string", "internal_cost": "number"}, FieldsByRole: map[string][]string{"operations": {"event_id", "event_time", "status", "internal_cost"}, "incident_diagnosis": {"event_id", "event_time", "status"}}, MaxRows: 10, MaxBytes: 8192, TimeoutSeconds: 2, MaxWindowSeconds: 3600, RetentionSeconds: 3600})
	}
	path := f.writeJSON("sources.json", descriptors)
	f.stopGateway()
	f.env["PRODUCT_SOURCE_REGISTRY"] = path
	f.stopGateway = f.start("gateway")
	f.wait("configured source gateway", func() bool {
		r, e := http.Get(f.endpoint + "/readyz")
		if e != nil {
			return false
		}
		defer r.Body.Close()
		return r.StatusCode == 200
	})
	diagnosticToken := "synthetic-diagnostic-token"
	if err := f.repo.BootstrapPrincipals(f.ctx, []domain.PrincipalBootstrap{{ID: "agent:fixture-diagnosis", Token: diagnosticToken, Roles: []string{"incident_diagnosis"}, ProjectIDs: []string{f.project}}}); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(time.Minute).Add(-time.Hour)
	q := domain.SourceQuery{ProjectID: f.project, ProductID: "checkout", SourceID: "log", Purpose: "diagnosis", Start: start, End: start.Add(30 * time.Minute), Fields: []string{"event_id", "event_time", "status"}, Limit: 10, IdempotencyKey: "source-read"}
	var listed struct {
		Sources []domain.SourceDescriptor `json:"sources"`
	}
	f.call(diagnosticToken, "product_source_list", map[string]any{"project_id": f.project}, true, &listed)
	if len(listed.Sources) != 5 {
		t.Fatal("permitted source discovery incomplete")
	}
	for _, d := range listed.Sources {
		if d.Endpoint != "" || d.TokenFile != "" {
			t.Fatal("discovery disclosed connector configuration")
		}
	}
	f.call(f.developer, "product_source_query", q, false, nil)
	f.call(f.foreign, "product_source_query", q, false, nil)
	bad := q
	bad.Fields = append(append([]string(nil), q.Fields...), "unapproved_personal_field")
	f.call(diagnosticToken, "product_source_query", bad, false, nil)
	if calls.Load() != 0 {
		t.Fatal("denied query reached source")
	}
	var receipt domain.SourceReceipt
	sourceRecords := map[string]string{}
	for _, kind := range []string{"log", "metric", "event", "lake", "audit"} {
		q.SourceID = kind
		f.call(diagnosticToken, "product_source_query", q, true, &receipt)
		sourceRecords[kind] = receipt.RecordID
		if receipt.RecordID == "" || receipt.Role != "incident_diagnosis" || receipt.Owner != "human:local-developer" {
			t.Fatalf("unattributed source receipt: %+v", receipt)
		}
		if kind == "lake" && receipt.Status != "partial" {
			t.Fatal("late lake data claimed complete")
		}
		if kind == "event" && receipt.Source.Offsets["orders-0"] != 7 {
			t.Fatal("event offset evidence missing")
		}
		var record domain.ProductRecord
		f.call(diagnosticToken, "product_record_get", map[string]any{"project_id": f.project, "record_id": receipt.RecordID, "current_only": true}, true, &record)
		if record.Status != "observed" || record.Origin != "source_adapter" || record.Source.ReceiptID != receipt.ID {
			t.Fatal("validated source observation not retained distinctly")
		}
	}
	before := calls.Load()
	previousID := receipt.ID
	f.call(diagnosticToken, "product_source_query", q, true, &receipt)
	if receipt.ID != previousID || calls.Load() != before {
		t.Fatal("repeated source query duplicated retained observation")
	}
	q.Limit = 9
	f.call(diagnosticToken, "product_source_query", q, false, nil)
	q.Limit = 10
	invalid.Store(true)
	q.IdempotencyKey = "invalid-response"
	f.call(diagnosticToken, "product_source_query", q, false, nil)
	receipt = domain.SourceReceipt{}
	f.call(diagnosticToken, "product_source_query", q, true, &receipt)
	if receipt.Status != "failed" || receipt.RecordID != "" || receipt.Evidence.SHA256 != "" {
		t.Fatal("invalid source payload retained as validated knowledge")
	}
	var observations, failed int
	if err := f.repo.Pool().QueryRow(f.ctx, `SELECT count(*) FROM product_records WHERE project_id=$1 AND kind='observation'`, f.project).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Pool().QueryRow(f.ctx, `SELECT count(*) FROM operation_records WHERE project_id=$1 AND record->>'name'='source.query' AND outcome IN ('failed','denied')`, f.project).Scan(&failed); err != nil {
		t.Fatal(err)
	}
	if observations != 5 || failed < 2 {
		t.Fatalf("retention/audit count: observations=%d failed=%d", observations, failed)
	}
	invalid.Store(false)
	privateQuery := q
	privateQuery.IdempotencyKey = "operations-only-fields"
	privateQuery.Fields = append(append([]string(nil), q.Fields...), "internal_cost")
	f.call(diagnosticToken, "product_source_query", privateQuery, false, nil)
	var privateReceipt domain.SourceReceipt
	f.call(f.operator, "product_source_query", privateQuery, true, &privateReceipt)
	f.call(diagnosticToken, "product_record_get", map[string]any{"project_id": f.project, "record_id": privateReceipt.RecordID, "current_only": true}, false, nil)
	var safeContext domain.ProductContext
	f.call(diagnosticToken, "product_context_search", domain.ProductContextRequest{ProjectID: f.project, ProductID: "checkout", RootIDs: []string{privateReceipt.RecordID}, Limit: 10, MaxBytes: 16000}, true, &safeContext)
	if len(safeContext.Records) != 0 {
		t.Fatal("KB retrieval bypassed source field restrictions")
	}
	var attributed int
	if err := f.repo.Pool().QueryRow(f.ctx, `SELECT count(*) FROM operation_records WHERE project_id=$1 AND record->>'name'='source.query' AND record->>'accountable_owner'='human:local-developer' AND record->>'role'='incident_diagnosis'`, f.project).Scan(&attributed); err != nil {
		t.Fatal(err)
	}
	if attributed == 0 {
		t.Fatal("source owner/role missing from audit")
	}
	var exposed int
	if err := f.repo.Pool().QueryRow(f.ctx, `SELECT count(*) FROM operation_records WHERE project_id=$1 AND record::text LIKE '%synthetic forbidden field%'`, f.project).Scan(&exposed); err != nil {
		t.Fatal(err)
	}
	if exposed != 0 {
		t.Fatal("sensitive source payload appeared in telemetry")
	}
	// Accepted intent binds exact BRS meaning and a server-executed oracle.
	accept := func(key, kind, content string, version int) domain.ProductRecord {
		var record domain.ProductRecord
		f.call(f.operator, "product_record_put", domain.ProductRecordInput{ProjectID: f.project, ProductID: "checkout", Key: key, Kind: kind, ExpectedVersion: version, Title: key, Content: content, Classification: "internal", SourceID: "fixture", SourceRevision: f.revision}, true, &record)
		var validation domain.ProductValidation
		f.call(f.operator, "product_record_validate", service.ValidateProductInput{ProjectID: f.project, RecordID: record.ID, ExpectedSHA256: record.SHA256, Evidence: []string{"Synthetic explicit acceptance criterion reviewed against fixture"}}, true, &validation)
		f.call(f.operator, "product_record_decide", domain.ProductDecision{ProjectID: f.project, RecordID: record.ID, ExpectedSHA256: record.SHA256, Decision: "accept", Reason: "Explicit fixture-only approval", ValidationID: validation.ID, IdempotencyKey: record.ID}, true, &record)
		return record
	}
	brs := accept("BRS-RECONCILE", "brs", "Every input order in the closed reporting window must have a processed audit result in that window.", 0)
	intentSpec := domain.IntentSpecification{Schema: domain.IntentSchema, Goal: "Check order processing against BRS", Stage: "diagnose", Bindings: []domain.ProductBinding{{RecordID: brs.ID, SHA256: brs.SHA256, Kind: brs.Kind}}, Criteria: []domain.IntentCriterion{{ID: "processed-orders", Statement: "Processed order outcomes reconcile", Oracle: "observation_reconciliation", Reconciliation: &domain.ReconciliationOracle{LeftSourceID: "event", RightSourceID: "audit", LeftSchemaVersion: "orders/v1", RightSchemaVersion: "orders/v1", JoinField: "event_id", ValueField: "status", ExpectedRightValue: "processed", MaxWindowSeconds: 3600}}}}
	raw, _ := json.Marshal(intentSpec)
	intent := accept("INTENT-RECONCILE", "intent", string(raw), 0)
	var ready domain.IntentContext
	preparedInput := service.IntentContextInput{ProjectID: f.project, IntentID: intent.ID, ExpectedSHA256: intent.SHA256}
	f.call(diagnosticToken, "product_intent_context", preparedInput, true, &ready)
	if !ready.Ready || len(ready.Records) != 1 {
		t.Fatal("exact accepted intent did not resolve")
	}
	comparison := service.CompareObservationsInput{ProjectID: f.project, IntentID: intent.ID, ExpectedSHA256: intent.SHA256, CriterionID: "processed-orders", LeftRecordID: sourceRecords["event"], RightRecordID: sourceRecords["audit"]}
	var evaluation domain.ObservationEvaluation
	f.call(f.developer, "product_observations_compare", comparison, false, nil)
	f.call(f.foreign, "product_observations_compare", comparison, false, nil)
	f.call(diagnosticToken, "product_observations_compare", comparison, true, &evaluation)
	if evaluation.Outcome != "violated" || evaluation.Mismatched != 1 {
		t.Fatalf("known business failure not detected: %+v", evaluation)
	}
	evaluationID := evaluation.ID
	f.call(diagnosticToken, "product_observations_compare", comparison, true, &evaluation)
	if evaluation.ID != evaluationID {
		t.Fatal("evaluation retry created duplicate evidence")
	}
	// This simulates changed source data; it is not a remediation-agent proof.
	processed.Store(true)
	q.SourceID, q.IdempotencyKey = "audit", "after-fixture-recovery"
	f.call(diagnosticToken, "product_source_query", q, true, &receipt)
	comparison.RightRecordID = receipt.RecordID
	f.call(diagnosticToken, "product_observations_compare", comparison, true, &evaluation)
	if evaluation.Outcome != "satisfied" || evaluation.Matched != 1 {
		t.Fatalf("reconciled window not recognized: %+v", evaluation)
	}
	goodEvaluation := evaluation.ID
	f.call(diagnosticToken, "product_evaluation_get", map[string]any{"project_id": f.project, "evaluation_id": goodEvaluation}, true, &evaluation)
	intentSpec.Criteria[0].Reconciliation.RightSourceID = "lake"
	raw, _ = json.Marshal(intentSpec)
	lakeIntent := accept("INTENT-LAKE", "intent", string(raw), 0)
	q.SourceID = "lake"
	f.call(diagnosticToken, "product_source_query", q, true, &receipt)
	comparison.IntentID, comparison.ExpectedSHA256, comparison.RightRecordID = lakeIntent.ID, lakeIntent.SHA256, receipt.RecordID
	f.call(diagnosticToken, "product_observations_compare", comparison, true, &evaluation)
	if evaluation.Outcome != "inconclusive" {
		t.Fatal("delayed lake data falsely proved recovery")
	}
	intentSpec.Assumptions = []domain.IntentAssumption{{Statement: "Source coverage assumption needs owner confirmation"}}
	raw, _ = json.Marshal(intentSpec)
	uncertain := accept("INTENT-UNCERTAIN", "intent", string(raw), 0)
	f.call(diagnosticToken, "product_intent_context", service.IntentContextInput{ProjectID: f.project, IntentID: uncertain.ID, ExpectedSHA256: uncertain.SHA256}, true, &ready)
	if ready.Ready || len(ready.Blockers) == 0 {
		t.Fatal("unconfirmed assumption silently accepted as execution context")
	}
	accept("BRS-RECONCILE", "brs", "Changed business meaning requires a new intent and evaluation.", 1)
	f.call(diagnosticToken, "product_intent_context", preparedInput, true, &ready)
	if ready.Ready {
		t.Fatal("superseded BRS silently reused")
	}
	f.call(diagnosticToken, "product_evaluation_get", map[string]any{"project_id": f.project, "evaluation_id": goodEvaluation}, false, nil)
	if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE product_evaluations SET evaluation='{}' WHERE id=$1`, goodEvaluation); err == nil {
		t.Fatal("immutable oracle report changed")
	}
	if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE principal_credentials SET expires_at=now()-interval '1 second' WHERE principal_id='agent:fixture-diagnosis'`); err != nil {
		t.Fatal(err)
	}
	status, _ := f.rpc(diagnosticToken, "tools/list", map[string]any{})
	if status != http.StatusUnauthorized {
		t.Fatal("expired diagnostic credential accepted")
	}
	// Diagnostic capability must not gain human validation/publication authority.
	if strings.Contains(receipt.Reason, "approved") {
		t.Fatal("failed observation represented as approval")
	}
}
