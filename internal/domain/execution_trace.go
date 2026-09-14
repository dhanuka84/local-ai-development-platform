package domain

import (
	"context"
	"time"
)

type ExecutionTraceInput struct {
	ProjectID string    `json:"project_id"`
	RunID     string    `json:"run_id"`
	Through   time.Time `json:"through,omitempty"`
	After     string    `json:"after,omitempty"`
}
type ExecutionTracePage struct {
	Records []OperationRecord `json:"records"`
	Through time.Time         `json:"through"`
	Next    string            `json:"next,omitempty"`
}
type ExecutionTraceRepository interface {
	ReadExecutionTrace(context.Context, ExecutionTraceInput) (ExecutionTracePage, error)
}
