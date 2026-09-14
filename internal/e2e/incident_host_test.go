//go:build sdlc_host

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
	"github.com/dhanuka84/hybrid-ai-platform/internal/sdlcworker"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"github.com/klauspost/compress/s2"
	"github.com/minio/minio-go/v7"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/encoding/protowire"
)

// The fixture's tiny order processor consumes actual Kafka input and writes
// its audit/lake/log/metric outputs. It is paused by an injected fault. Only
// the separately credentialed remedy endpoint can change that pause state.
func TestSDLCIncidentNativeRecoveryE2E(t *testing.T) {
	f := startRuntime(t)
	native := setupNativeSources(t, f, true)
	tokens, registry := configureExecutionFixture(t, f)
	var paused, late, failRemedy atomic.Bool
	paused.Store(true)
	late.Store(true)
	failRemedy.Store(true)
	var stateMu sync.Mutex
	action := ""
	writes := 0
	state := func() []byte {
		if paused.Load() {
			return []byte(`{"paused":true}`)
		}
		return []byte(`{"paused":false}`)
	}
	remedyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		defer stateMu.Unlock()
		if r.Header.Get("Authorization") != "Bearer synthetic-remedy-only" {
			http.Error(w, "denied", 403)
			return
		}
		current := state()
		if r.Method == "PUT" {
			if failRemedy.Load() {
				http.Error(w, "injected action failure", 503)
				return
			}
			if r.Header.Get("If-Match") != `"`+domain.Digest(current)+`"` || r.Header.Get("Idempotency-Key") == "" {
				http.Error(w, "stale", 412)
				return
			}
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 1024))
			if string(raw) != `{"paused":false}` {
				http.Error(w, "outside fixed capability", 403)
				return
			}
			paused.Store(false)
			action = r.Header.Get("Idempotency-Key")
			writes++
			current = state()
		}
		w.Header().Set("ETag", `"`+domain.Digest(current)+`"`)
		w.Header().Set("X-SDLC-Action", action)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(current)
	}))
	t.Cleanup(remedyServer.Close)
	remedy := execution.RemediationConfig{Remedies: []execution.Remedy{{ID: "resume-order-processor", Endpoint: remedyServer.URL, TokenFile: f.write("remedy.token", []byte("synthetic-remedy-only")), Before: json.RawMessage(`{"paused":true}`), Desired: json.RawMessage(`{"paused":false}`)}}}
	registry.Targets[0].Sources = []string{"event", "lake", "audit", "log", "metric"}
	registry.Targets[0].AllowedRemedies = []string{"resume-order-processor"}
	registry.Targets[0].RemediationSHA256 = remedy.Digest()
	registry.Targets[0].ObservationSeconds = 4
	registry.Targets[0].Budget.MaxSourceQueries = 30
	registry.Targets[0].Budget.MaxSourceRows = 3000
	f.stopGateway()
	f.env["SDLC_RUNTIME_REGISTRY"] = f.writeJSON("incident-registry.json", registry)
	f.stopGateway = f.start("gateway")
	waitExecutionGateway(f)

	var productionMu sync.Mutex
	produced := map[string]bool{}
	var metricsEnd time.Time
	produce := func(q domain.SourceQuery) error {
		productionMu.Lock()
		defer productionMu.Unlock()
		key := q.Start.Format(time.RFC3339Nano) + q.End.Format(time.RFC3339Nano)
		if produced[key] {
			return nil
		}
		when := q.Start.Add(time.Second)
		id := "order-" + strconv.FormatInt(when.UnixNano(), 10)
		raw, _ := json.Marshal(map[string]any{"event_id": id, "event_time": when, "status": "accepted"})
		input := &kgo.Record{Topic: native.topic, Value: raw}
		if err := native.client.ProduceSync(f.ctx, input, &kgo.Record{Topic: native.topic, Headers: []kgo.RecordHeader{{Key: "hybrid-ai-watermark", Value: []byte(q.End.Format(time.RFC3339Nano))}}}).FirstErr(); err != nil {
			return err
		}
		status := "blocked"
		if !paused.Load() {
			consumer, err := kgo.NewClient(kgo.SeedBrokers(os.Getenv("TEST_KAFKA_ADDRESS")), kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{native.topic: {0: kgo.NewOffset().At(input.Offset)}}))
			if err != nil {
				return err
			}
			fetches := consumer.PollRecords(f.ctx, 1)
			consumer.Close()
			if len(fetches.Errors()) > 0 || len(fetches.Records()) != 1 || !bytes.Equal(fetches.Records()[0].Value, raw) {
				return fmt.Errorf("synthetic processor did not consume the actual input")
			}
			status = "processed"
		}
		if _, err := f.repo.Pool().Exec(f.ctx, `INSERT INTO native_orders VALUES($1,$2,$3)`, id, when, status); err != nil {
			return err
		}
		if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE native_coverage SET watermark=$1`, q.End); err != nil {
			return err
		}
		rows, err := f.repo.Pool().Query(f.ctx, `SELECT row_to_json(r) FROM (SELECT * FROM native_orders WHERE event_time>=$1 AND event_time<$2 ORDER BY event_time) r`, q.Start, q.End)
		if err != nil {
			return err
		}
		lakeRows := []map[string]json.RawMessage{}
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return err
			}
			var row map[string]json.RawMessage
			_ = json.Unmarshal(raw, &row)
			lakeRows = append(lakeRows, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		watermark := q.End
		if late.Load() {
			watermark = q.Start
		}
		envelope, _ := json.Marshal(domain.SourceEnvelope{SchemaVersion: "orders/v1", Revision: id, Start: q.Start, End: q.End, Watermark: watermark, Complete: true, Rows: lakeRows})
		if _, err = native.lake.PutObject(f.ctx, native.bucket, "orders.json", bytes.NewReader(envelope), int64(len(envelope)), minio.PutObjectOptions{ContentType: "application/json"}); err != nil {
			return err
		}
		logRow, _ := json.Marshal(map[string]any{"event_id": id, "event_time": when, "status": map[bool]string{true: "processor-paused", false: "processor-running"}[paused.Load()]})
		barrier, _ := json.Marshal(map[string]any{"_watermark": q.End})
		push, _ := json.Marshal(map[string]any{"streams": []any{map[string]any{"stream": map[string]string{"stream": native.topic}, "values": [][]string{{strconv.FormatInt(when.UnixNano(), 10), string(logRow)}, {strconv.FormatInt(q.End.Add(-time.Nanosecond).UnixNano(), 10), string(barrier)}}}}})
		response, err := http.Post(os.Getenv("TEST_LOKI_ENDPOINT")+"/loki/api/v1/push", "application/json", bytes.NewReader(push))
		if err != nil {
			return err
		}
		_ = response.Body.Close()
		if response.StatusCode != 204 {
			return fmt.Errorf("native log producer failed")
		}
		value := float64(0)
		if status == "processed" {
			value = 1
		}
		metricsStart := q.Start
		if metricsEnd.After(metricsStart) {
			metricsStart = metricsEnd
		}
		if err = writeFixtureMetrics(f, metricsStart, q.End, value); err != nil {
			return err
		}
		metricsEnd = q.End
		produced[key] = true
		return nil
	}
	// Data is generated on demand for the bounded observation window, then read
	// through the shipped native adapter. No source response is fabricated here.
	upstream := native.contracts[0].Endpoint
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 16384))
		var q domain.SourceQuery
		if json.Unmarshal(raw, &q) != nil {
			http.Error(w, "bad query", 400)
			return
		}
		if err := produce(q); err != nil {
			t.Logf("synthetic producer failed: %v", err)
			http.Error(w, "synthetic producer failed: "+err.Error(), 500)
			return
		}
		u, _ := url.Parse(upstream)
		request, _ := http.NewRequestWithContext(r.Context(), "POST", u.String(), bytes.NewReader(raw))
		request.Header = r.Header.Clone()
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			http.Error(w, "adapter unavailable", 502)
			return
		}
		defer response.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	t.Cleanup(proxy.Close)
	for i := range native.contracts {
		native.contracts[i].Endpoint = proxy.URL
	}
	f.stopGateway()
	f.env["PRODUCT_SOURCE_REGISTRY"] = f.writeJSON("incident-sources.json", native.contracts)
	f.stopGateway = f.start("gateway")
	waitExecutionGateway(f)
	brs := acceptExecutionFixture(f, "BRS-orders", "brs", "Every accepted input order must appear as processed in audit and lake. Healthy infrastructure alone does not prove business recovery.", 0)
	historical := acceptExecutionFixture(f, "INCIDENT-history", "incident", "A historical incident with similar symptoms was caused by database outage. This is historical evidence only; current audit connectivity and processor state must be checked.", 0)
	spec := domain.IntentSpecification{Schema: domain.IntentSchema, Goal: "Diagnose and restore complete order processing", Stage: "diagnose", Bindings: []domain.ProductBinding{{RecordID: brs.ID, SHA256: brs.SHA256, Kind: brs.Kind}, {RecordID: historical.ID, SHA256: historical.SHA256, Kind: historical.Kind}}}
	for _, right := range []string{"audit", "lake"} {
		spec.Criteria = append(spec.Criteria, domain.IntentCriterion{ID: "processed-" + right, Statement: "Input orders are processed in " + right, Oracle: "observation_reconciliation", Reconciliation: &domain.ReconciliationOracle{LeftSourceID: "event", RightSourceID: right, LeftSchemaVersion: "orders/v1", RightSchemaVersion: "orders/v1", JoinField: "event_id", ValueField: "status", ExpectedRightValue: "processed", MaxWindowSeconds: 60}})
	}
	raw, _ := json.Marshal(spec)
	intent := acceptExecutionFixture(f, "INTENT-orders", "intent", string(raw), 0)
	var run domain.ExecutionRun
	f.call(f.operator, "sdlc_run_create", service.ExecutionCreateInput{ProjectID: f.project, TargetID: "synthetic-target", Kind: "incident", IntentID: intent.ID, ExpectedSHA256: intent.SHA256, IdempotencyKey: "native-incident"}, true, &run)
	defer retainExecutionFixture(t, f, run.ID)
	invoke := func(role string) {
		t.Helper()
		cfg := sdlcworker.Config{ProjectID: f.project, MCPURL: f.endpoint + "/mcp", TokenFile: f.write(role+".token", []byte(tokens[role])), Role: role, OllamaURL: f.env["OLLAMA_URL"], SpoolDirectory: filepath.Join(f.root, "incident-"+role), TrialKind: "fixture", Remediation: remedy}
		cmd := f.command("sdlc-worker", "", "--config", f.writeJSON(role+".json", cfg), "--run", run.ID)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
		raw, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("incident %s worker: %v %s", role, err, raw)
		}
		if json.Unmarshal(raw, &run) != nil {
			t.Fatal("invalid incident result")
		}
	}
	resume := func() {
		t.Helper()
		f.call(f.operator, "sdlc_run_control", service.ExecutionControlInput{ExecutionIDInput: service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}, ExpectedVersion: run.Version, Action: "resume", Reason: "Synthetic fault prerequisite resolved; retry under the same accepted intent"}, true, &run)
	}
	invoke("sdlc_diagnosis")
	if run.Status != "blocked" || run.Stage != "diagnose" {
		t.Fatal("delayed native lake did not block diagnosis", run.Blockers)
	}
	late.Store(false)
	resume()
	time.Sleep(time.Second)
	f.generationCallbacks <- func(request []byte) []byte {
		var model execution.ModelRequest
		_ = json.Unmarshal(request, &model)
		var prompt struct {
			Observations []domain.ProductRecord         `json:"observations"`
			Evaluations  []domain.ObservationEvaluation `json:"evaluations"`
		}
		_ = json.Unmarshal([]byte(model.Prompt), &prompt)
		ids := map[string]string{}
		for _, r := range prompt.Observations {
			ids[r.Source.SourceID] = r.ID
		}
		proposal := execution.DiagnosisProposal{Schema: "hybrid-ai/diagnosis-proposal/v1", Criteria: []string{"processed-audit", "processed-lake"}, Summary: "Current logs show a paused processor, while readable audit data contradicts the historical database-outage explanation.", Hypotheses: []domain.ExecutionHypothesis{{ID: "paused-processor", Explanation: "Current processor state and business reconciliation indicate the order consumer is paused.", Supports: []string{ids["log"], ids["event"], prompt.Evaluations[0].ID}, Status: "supported", ProposedRemedy: "resume-order-processor"}, {ID: "database-outage", Explanation: "The historical outage explanation is contradicted by current readable audit records.", Supports: []string{historical.ID}, Contradicts: []string{ids["audit"]}, Status: "contradicted"}}}
		raw, _ := json.Marshal(proposal)
		response, _ := json.Marshal(execution.ModelResponse{Model: model.Model, Response: string(raw), Done: true, DoneReason: "stop", PromptEvalCount: 900, EvalCount: 400})
		return response
	}
	invoke("sdlc_diagnosis")
	if run.Stage != "remediate" || run.Status != "ready" {
		t.Fatal("native evidence did not yield governed diagnosis", run.Blockers)
	}
	f.call(tokens["sdlc_diagnosis"], "sdlc_step_claim", service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}, false, nil)
	invoke("sdlc_remediation")
	if run.Status != "blocked" || !paused.Load() {
		t.Fatal("failed remedy was credited as recovery")
	}
	failRemedy.Store(false)
	resume()
	invoke("sdlc_remediation")
	if run.Stage != "verify_recovery" || run.Status != "ready" {
		t.Fatal("fixed remedy did not execute", run.Blockers)
	}
	invoke("sdlc_evaluator")
	if run.Status != "completed" {
		t.Fatal("native technical and business recovery failed", run.Blockers)
	}
	stateMu.Lock()
	count := writes
	stateMu.Unlock()
	if count != 1 {
		t.Fatal("remedy duplicated its external effect", count)
	}
	var view domain.ExecutionView
	f.call(f.operator, "sdlc_run_get", service.ExecutionIDInput{ProjectID: f.project, RunID: run.ID}, true, &view)
	if len(view.Steps) != 5 || view.Steps[1].Actor == view.Steps[3].Actor || view.Steps[3].Actor == view.Steps[4].Actor || len(view.Steps[4].Result.EvaluationIDs) != 2 {
		t.Fatal("incident independence or business proof missing")
	}
	// Model prompts contain scoped observations; a workload cannot retrieve
	// them through a generic artifact route after its diagnostic lease ends.
	f.call(tokens["sdlc_diagnosis"], "sdlc_artifact_get", map[string]any{"project_id": f.project, "run_id": run.ID, "sha256": view.Steps[1].Result.Model.Prompt.SHA256}, false, nil)
	registry.Targets[0].Budget.MaxSourceQueries = 1
	f.stopGateway()
	f.env["SDLC_RUNTIME_REGISTRY"] = f.writeJSON("execution-registry.json", registry)
	f.stopGateway = f.start("gateway")
	waitExecutionGateway(f)
	var limited domain.ExecutionRun
	f.call(f.operator, "sdlc_run_create", service.ExecutionCreateInput{ProjectID: f.project, TargetID: "synthetic-target", Kind: "incident", IntentID: intent.ID, ExpectedSHA256: intent.SHA256, IdempotencyKey: "incident-source-budget"}, true, &limited)
	var claim domain.ExecutionClaim
	f.call(tokens["sdlc_diagnosis"], "sdlc_step_claim", service.ExecutionIDInput{ProjectID: f.project, RunID: limited.ID}, true, &claim)
	lease := domain.ExecutionLease{ProjectID: f.project, RunID: limited.ID, StepID: claim.Step.ID, Fence: claim.Step.Fence, Token: claim.Token}
	var c service.ExecutionContext
	f.call(tokens["sdlc_diagnosis"], "sdlc_step_context", lease, true, &c)
	q := domain.SourceQuery{ProjectID: f.project, ProductID: "labels", SourceID: "event", Purpose: "diagnosis", Start: c.ObservationStart, End: c.ObservationEnd, Fields: []string{"event_id", "event_time", "status"}, Limit: 10, IdempotencyKey: "budget-once"}
	input := struct {
		domain.ExecutionLease
		Query domain.SourceQuery `json:"query"`
	}{lease, q}
	var receipt, again domain.SourceReceipt
	f.call(tokens["sdlc_diagnosis"], "sdlc_source_query", input, true, &receipt)
	f.call(tokens["sdlc_diagnosis"], "sdlc_source_query", input, true, &again)
	if receipt.ID != again.ID || receipt.Status != "complete" {
		t.Fatal("source replay lost exact receipt")
	}
	input.Query.IdempotencyKey = "budget-overflow"
	f.call(tokens["sdlc_diagnosis"], "sdlc_source_query", input, false, nil)
	if _, err := f.repo.Pool().Exec(f.ctx, `UPDATE principal_credentials SET expires_at=now()-interval '1 second' WHERE principal_id=$1`, limited.Target.Participants["sdlc_diagnosis"]); err != nil {
		t.Fatal(err)
	}
	f.stopGateway()
	f.stopGateway = f.start("gateway")
	waitExecutionGateway(f)
	status, _ := f.rpc(tokens["sdlc_diagnosis"], "tools/call", map[string]any{"name": "sdlc_record_get", "arguments": map[string]any{"project_id": f.project, "run_id": limited.ID, "step_id": lease.StepID, "fence": lease.Fence, "lease_token": lease.Token, "record_id": receipt.RecordID}})
	if status != http.StatusUnauthorized {
		t.Fatal("expired incident authority was resurrected")
	}
}

func writeFixtureMetrics(f *runtime, start, end time.Time, value float64) error {
	field := func(dst []byte, num protowire.Number, raw []byte) []byte {
		dst = protowire.AppendTag(dst, num, protowire.BytesType)
		return protowire.AppendBytes(dst, raw)
	}
	var request []byte
	for _, metric := range []struct {
		name  string
		value float64
	}{{"synthetic_processed", value}, {"synthetic_watermark", float64(end.Unix())}} {
		var series []byte
		for _, label := range [][2]string{{"__name__", metric.name}, {"project", f.project}} {
			var raw []byte
			raw = field(raw, 1, []byte(label[0]))
			raw = field(raw, 2, []byte(label[1]))
			series = field(series, 1, raw)
		}
		for when := start; when.Before(end); when = when.Add(time.Second) {
			var sample []byte
			sample = protowire.AppendTag(sample, 1, protowire.Fixed64Type)
			sample = protowire.AppendFixed64(sample, math.Float64bits(metric.value))
			sample = protowire.AppendTag(sample, 2, protowire.VarintType)
			sample = protowire.AppendVarint(sample, uint64(when.UnixMilli()))
			series = field(series, 2, sample)
		}
		request = field(request, 1, series)
	}
	req, err := http.NewRequestWithContext(f.ctx, "POST", os.Getenv("TEST_PROMETHEUS_ENDPOINT")+"/api/v1/write", bytes.NewReader(s2.EncodeSnappy(nil, request)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 204 {
		raw, _ := io.ReadAll(response.Body)
		return fmt.Errorf("native metric producer: %d %s", response.StatusCode, raw)
	}
	return nil
}
