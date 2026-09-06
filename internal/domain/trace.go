package domain

import (
	"context"
	"time"
)

// Operation metadata is deliberately typed: no prompts, arbitrary payloads,
// credentials, raw output, or model reasoning can be added to telemetry.
type OperationScope struct {
	ProjectID  string `json:"project_id,omitempty"`
	WorkflowID string `json:"workflow_id,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
}
type EvidenceReference struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int    `json:"version,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
}
type OperationRecord struct {
	ID          string `json:"id"`
	OperationID string `json:"operation_id"`
	OperationScope
	TraceID        string              `json:"trace_id"`
	SpanID         string              `json:"span_id"`
	ParentSpanID   string              `json:"parent_span_id,omitempty"`
	Name           string              `json:"name"`
	Phase          string              `json:"phase"`
	Outcome        string              `json:"outcome"`
	Actor          string              `json:"actor"`
	Role           string              `json:"role,omitempty"`
	InputSHA256    string              `json:"input_sha256"`
	ResultSHA256   string              `json:"result_sha256,omitempty"`
	References     []EvidenceReference `json:"references"`
	PolicyVersion  string              `json:"policy_version,omitempty"`
	PolicyDecision string              `json:"policy_decision,omitempty"`
	Provider       string              `json:"provider,omitempty"`
	Model          string              `json:"model,omitempty"`
	Rationale      string              `json:"rationale"`
	RecordedAt     time.Time           `json:"recorded_at"`
}
type WorkflowTrace struct {
	WorkflowID string            `json:"workflow_id"`
	ProjectID  string            `json:"project_id"`
	Records    []OperationRecord `json:"records"`
	Complete   bool              `json:"complete"`
	Missing    []string          `json:"missing"`
	Coverage   string            `json:"coverage"`
}
type TraceRepository interface {
	AppendOperation(context.Context, OperationRecord) error
	ReadWorkflowTrace(context.Context, string, string, int) (WorkflowTrace, error)
	PendingTraceExports(context.Context, int) ([]OperationRecord, error)
	FinishTraceExport(context.Context, string, bool) error
}
type operationScopeKey struct{}

func WithOperationScope(ctx context.Context, scope OperationScope) context.Context {
	return context.WithValue(ctx, operationScopeKey{}, scope)
}
func ScopeFromContext(ctx context.Context) OperationScope {
	scope, _ := ctx.Value(operationScopeKey{}).(OperationScope)
	return scope
}
