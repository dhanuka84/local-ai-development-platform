package graph

import (
	"context"
	"log/slog"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type FallbackStore struct {
	primary  domain.GraphStore
	fallback domain.GraphStore
	logger   *slog.Logger
}

func WithFallback(primary, fallback domain.GraphStore, logger *slog.Logger) *FallbackStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &FallbackStore{primary: primary, fallback: fallback, logger: logger}
}

func (s *FallbackStore) ExpandRepositoryGraph(ctx context.Context, request domain.RepositoryGraphRequest) ([]domain.RepositoryRelation, error) {
	result, err := s.primary.ExpandRepositoryGraph(ctx, request)
	if err == nil {
		return result, nil
	}
	s.logger.Warn("AGE repository traversal failed; using PostgreSQL recursive fallback", "error", err)
	return s.fallback.ExpandRepositoryGraph(ctx, request)
}

func (s *FallbackStore) ExpandCodeGraph(ctx context.Context, request domain.CodeGraphRequest) (domain.CodeGraph, error) {
	result, err := s.primary.ExpandCodeGraph(ctx, request)
	if err == nil {
		return result, nil
	}
	s.logger.Warn("AGE code traversal failed; using PostgreSQL recursive fallback", "error", err)
	return s.fallback.ExpandCodeGraph(ctx, request)
}

func (s *FallbackStore) ExpandKnowledgeGraph(ctx context.Context, request domain.KnowledgeGraphRequest) (domain.KnowledgeSubgraph, error) {
	result, err := s.primary.ExpandKnowledgeGraph(ctx, request)
	if err == nil {
		return result, nil
	}
	s.logger.Warn("AGE unified traversal failed; using PostgreSQL recursive fallback", "error", err)
	return s.fallback.ExpandKnowledgeGraph(ctx, request)
}

var _ domain.GraphStore = (*FallbackStore)(nil)
