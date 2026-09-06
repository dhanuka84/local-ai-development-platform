package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type Exporter struct {
	repository domain.TraceRepository
	endpoint   string
	client     *http.Client
}

func NewExporter(repository domain.TraceRepository, endpoint string) (*Exporter, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("trace export requires a local HTTP collector")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && host != "otel-collector" && !(ip != nil && ip.IsLoopback()) {
		return nil, fmt.Errorf("trace export is restricted to localhost or the local otel-collector service")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/v1/traces"
	}
	if parsed.Path != "/v1/traces" {
		return nil, fmt.Errorf("trace export path must be /v1/traces")
	}
	return &Exporter{repository: repository, endpoint: parsed.String(), client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (e *Exporter) ProcessOnce(ctx context.Context) error {
	records, err := e.repository.PendingTraceExports(ctx, 25)
	if err != nil {
		return err
	}
	for _, record := range records {
		data, err := exportJSON(record)
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(data))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		response, callErr := e.client.Do(request)
		success := callErr == nil && response.StatusCode >= 200 && response.StatusCode < 300
		if response != nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
			if readErr != nil || len(body) > 4096 {
				success = false
			}
			if success && len(bytes.TrimSpace(body)) > 0 {
				var receipt struct {
					PartialSuccess struct {
						RejectedSpans json.Number `json:"rejectedSpans"`
						ErrorMessage  string      `json:"errorMessage"`
					} `json:"partialSuccess"`
				}
				if json.Unmarshal(body, &receipt) != nil || (receipt.PartialSuccess.RejectedSpans != "" && receipt.PartialSuccess.RejectedSpans != "0") || receipt.PartialSuccess.ErrorMessage != "" {
					success = false
				}
			}
			_ = response.Body.Close()
		}
		if err := e.repository.FinishTraceExport(ctx, record.ID, success); err != nil {
			return err
		}
	}
	return nil
}

// OTLP/HTTP JSON uses hexadecimal trace/span IDs and string-encoded nanoseconds.
// Each immutable receipt is exported as its own child span, not an invented
// end-to-end latency measurement. The parent is the owned operation span.
func exportJSON(record domain.OperationRecord) ([]byte, error) {
	attrs := []map[string]any{}
	for key, value := range map[string]string{"operation.id": record.OperationID, "operation.phase": record.Phase, "operation.outcome": record.Outcome, "project.id": record.ProjectID, "workflow.id": record.WorkflowID, "task.id": record.TaskID, "policy.version": record.PolicyVersion, "policy.decision": record.PolicyDecision, "provider": record.Provider, "model": record.Model} {
		if value != "" {
			attrs = append(attrs, map[string]any{"key": key, "value": map[string]string{"stringValue": value}})
		}
	}
	recordID := strings.ReplaceAll(record.ID, "-", "")
	if len(recordID) != 32 || len(record.TraceID) != 32 || len(record.SpanID) != 16 {
		return nil, fmt.Errorf("invalid trace identifiers")
	}
	spanID := recordID[:16]
	span := map[string]any{"traceId": record.TraceID, "spanId": spanID, "parentSpanId": record.SpanID, "name": record.Name + "." + record.Phase, "kind": 1, "startTimeUnixNano": strconv.FormatInt(record.RecordedAt.UnixNano(), 10), "endTimeUnixNano": strconv.FormatInt(record.RecordedAt.Add(time.Nanosecond).UnixNano(), 10), "attributes": attrs}
	return json.Marshal(map[string]any{"resourceSpans": []any{map[string]any{"resource": map[string]any{"attributes": []any{map[string]any{"key": "service.name", "value": map[string]string{"stringValue": "hybrid-ai-platform"}}}}, "scopeSpans": []any{map[string]any{"scope": map[string]string{"name": "hybrid-ai/durable-evidence"}, "spans": []any{span}}}}}})
}
