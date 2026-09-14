package service

import (
	"context"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func (s *Service) ExecutionTrace(ctx context.Context, in domain.ExecutionTraceInput) (domain.ExecutionTracePage, error) {
	if in.Through.IsZero() {
		in.Through = time.Now().UTC()
	}
	if in.Through.After(time.Now().Add(time.Second)) || len(in.After) > 128 {
		return domain.ExecutionTracePage{}, ErrInvalidInput
	}
	if _, err := s.GetExecution(ctx, ExecutionIDInput{ProjectID: in.ProjectID, RunID: in.RunID}); err != nil {
		return domain.ExecutionTracePage{}, err
	}
	r, ok := s.repository.(domain.ExecutionTraceRepository)
	if !ok {
		return domain.ExecutionTracePage{}, domain.ErrEvidenceUnavailable
	}
	return r.ReadExecutionTrace(ctx, in)
}
