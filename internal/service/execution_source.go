package service

import (
	"context"
	"slices"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

// The execution-authorized attribute is built only from a live server-checked
// lease. A direct generic source tool call cannot set it or acquire authority.
func (s *Service) authorizeExecutionSource(ctx context.Context, d domain.SourceDescriptor, action, purpose string) (domain.Principal, error) {
	attrs := map[string]any{"classification": d.Classification, "purpose": purpose, "execution_authorized": false}
	if run, ok := executionGrant(ctx); ok {
		if run.ProjectID != d.ProjectID || run.ProductID != d.ProductID || !slices.Contains(run.Target.Sources, d.ID) || !slices.Contains(run.Target.Purposes, purpose) || !slices.Contains(run.Target.Classifications, d.Classification) || !slices.Contains([]string{"diagnose", "verify_recovery"}, run.Stage) {
			return domain.Principal{}, ErrForbidden
		}
		attrs["execution_authorized"] = true
		attrs["environment"] = run.Target.Environment
	}
	return s.AuthorizeProjectAction(ctx, d.ProjectID, "product_source", d.ID, action, attrs)
}
