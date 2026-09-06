package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
	if _, err := wrapped.Embed(context.Background(), []string{"secret raw prompt"}); !errors.Is(err, domain.ErrEvidenceUnavailable) || e.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, e.calls)
	}
	r.fail = false
	if _, err := wrapped.Embed(context.Background(), []string{"secret raw prompt"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(r.records)
	if strings.Contains(string(raw), "secret raw prompt") {
		t.Fatal("trace leaked prompt")
	}
	if len(r.records) != 2 || r.records[0].TraceID != r.records[1].TraceID || r.records[0].SpanID != r.records[1].SpanID {
		t.Fatal("uncorrelated operation")
	}
}
func TestLocalExportFailureAndPartialRejectionRemainRetryable(t *testing.T) {
	r := &testRepository{}
	_, finish, err := Begin(context.Background(), r, "test", domain.OperationScope{ProjectID: "fixture"}, nil)
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
	if err = exporter.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, success := range r.exports {
		if success {
			t.Fatal("partial failure marked exported")
		}
	}
	r.exports = nil
	response = "{}"
	if err = exporter.ProcessOnce(context.Background()); err != nil {
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
