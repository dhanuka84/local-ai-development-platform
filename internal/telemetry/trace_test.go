package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func TestRecordsOwnTheirEvidenceReferences(t *testing.T) {
	refs := []domain.EvidenceReference{{Kind: "artifact", ID: "intent"}}
	_, span, intent := Start(t.Context(), "test", domain.OperationScope{}, refs)
	defer span.End()
	refs[0].ID = "caller-mutated"
	if intent.References[0].ID != "intent" {
		t.Fatal("Start retained caller-owned reference storage")
	}

	// Spare capacity exposes accidental reuse by append in Result.
	intent.References = make([]domain.EvidenceReference, 1, 4)
	intent.References[0] = domain.EvidenceReference{Kind: "artifact", ID: "intent"}
	outcome := Result(intent, "success", []domain.EvidenceReference{{Kind: "artifact", ID: "outcome"}})
	outcome.References[0].ID = "outcome-mutated"
	if intent.References[0].ID != "intent" || intent.References[:2][1] != (domain.EvidenceReference{}) {
		t.Fatal("Result mutated intent reference storage")
	}
	if outcome.References[1].ID != "outcome" {
		t.Fatal("Result lost outcome evidence")
	}
}

func TestExportAttributesHaveStableOrder(t *testing.T) {
	id, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := exportJSON(domain.OperationRecord{
		ID: id, OperationID: "operation", OperationScope: domain.OperationScope{ProjectID: "project"},
		TraceID: strings.Repeat("a", 32), SpanID: strings.Repeat("b", 16),
		Phase: "intent", Outcome: "pending", RecordedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []struct {
					Attributes []struct {
						Key string `json:"key"`
					} `json:"attributes"`
				} `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.ResourceSpans) != 1 || len(payload.ResourceSpans[0].ScopeSpans) != 1 || len(payload.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
		t.Fatalf("unexpected OTLP envelope: %s", raw)
	}
	var keys []string
	for _, attribute := range payload.ResourceSpans[0].ScopeSpans[0].Spans[0].Attributes {
		keys = append(keys, attribute.Key)
	}
	if want := []string{"operation.id", "operation.outcome", "operation.phase", "project.id"}; !slices.Equal(keys, want) {
		t.Fatalf("OTLP attribute keys = %q; want %q", keys, want)
	}
}

type testRepository struct {
	domain.TraceRepository
	records []domain.OperationRecord
	fail    bool
	exports []bool
}

func (r *testRepository) AppendOperation(_ context.Context, v domain.OperationRecord) error {
	if r.fail {
		return domain.ErrEvidenceUnavailable
	}
	r.records = append(r.records, v)
	return nil
}
func (r *testRepository) PendingTraceExports(context.Context, int) ([]domain.OperationRecord, error) {
	return r.records, nil
}
func (r *testRepository) FinishTraceExport(_ context.Context, _ string, ok bool) error {
	r.exports = append(r.exports, ok)
	return nil
}

type testEmbedder struct{ calls int }

func (e *testEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	e.calls++
	return [][]float32{{1, 2}}, nil
}
func (e *testEmbedder) Ping(context.Context) error { return nil }
func TestEvidenceFailurePreventsModelEffect(t *testing.T) {
	r := &testRepository{fail: true}
	e := &testEmbedder{}
	wrapped := WrapEmbedder(r, e)
	if _, err := wrapped.Embed(t.Context(), []string{"secret raw prompt"}); !errors.Is(err, domain.ErrEvidenceUnavailable) || e.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, e.calls)
	}
	r.fail = false
	if _, err := wrapped.Embed(t.Context(), []string{"secret raw prompt"}); err != nil {
		t.Fatal(err)
	}
	raw, marshalErr := json.Marshal(r.records)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(raw), "secret raw prompt") {
		t.Fatal("trace leaked prompt")
	}
	if len(r.records) != 2 || r.records[0].TraceID != r.records[1].TraceID || r.records[0].SpanID != r.records[1].SpanID {
		t.Fatal("uncorrelated operation")
	}
}
func TestLocalExportFailureAndPartialRejectionRemainRetryable(t *testing.T) {
	r := &testRepository{}
	_, finish, err := Begin(t.Context(), r, "test", domain.OperationScope{ProjectID: "fixture"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = finish(nil); err != nil {
		t.Fatal(err)
	}
	response := `{"partialSuccess":{"rejectedSpans":"1","errorMessage":"retry"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/traces" {
			t.Error("wrong OTLP path")
		}
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	exporter, err := NewExporter(r, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err = exporter.ProcessOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, success := range r.exports {
		if success {
			t.Fatal("partial failure marked exported")
		}
	}
	r.exports = nil
	response = "{}"
	if err = exporter.ProcessOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, success := range r.exports {
		if !success {
			t.Fatal("successful export not recorded")
		}
	}
	if _, err = NewExporter(r, "https://cloud.example/v1/traces"); err == nil {
		t.Fatal("cloud exporter allowed")
	}
	if _, err = exportJSON(domain.OperationRecord{}); err == nil {
		t.Fatal("invalid identifiers accepted")
	}
}
