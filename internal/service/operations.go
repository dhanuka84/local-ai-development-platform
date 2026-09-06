package service

import (
	"context"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
)

func (s *Service) operations(ctx context.Context, project string) (domain.OperationsRepository, error) {
	p, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if !p.HasRole(project, "operations") {
		return nil, ErrForbidden
	}
	if _, err = s.AuthorizeProjectAction(ctx, project, "knowledge_candidate", "operations", "read", nil); err != nil {
		return nil, err
	}
	r, ok := s.repository.(domain.OperationsRepository)
	if !ok {
		return nil, domain.ErrEvidenceUnavailable
	}
	return r, nil
}
func (s *Service) FailedKnowledgeIndexes(ctx context.Context, project string) ([]domain.FailedIndex, error) {
	r, err := s.operations(ctx, project)
	if err != nil {
		return nil, err
	}
	return r.FailedKnowledgeIndexes(ctx, project)
}
func (s *Service) RetryKnowledgeIndex(ctx context.Context, in domain.RetryIndexInput) (int64, error) {
	r, err := s.operations(ctx, in.ProjectID)
	if err != nil {
		return 0, err
	}
	p, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return 0, err
	}
	if !p.Human {
		return 0, ErrForbidden
	}
	in.Actor = p.ID
	return r.RetryKnowledgeIndex(ctx, in)
}
func (s *Service) EvidenceHealth(ctx context.Context, project string) (domain.EvidenceHealth, error) {
	r, err := s.operations(ctx, project)
	if err != nil {
		return domain.EvidenceHealth{}, err
	}
	days := s.traceRetentionDays
	if days == 0 {
		days = 30
	}
	return r.EvidenceHealth(ctx, project, days)
}
func (s *Service) ConfigureTraceRetention(days int) error {
	if days < 1 || days > 3650 {
		return ErrInvalidInput
	}
	s.traceRetentionDays = days
	return nil
}
