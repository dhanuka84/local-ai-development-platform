package platform

import (
	"context"
	"fmt"

	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
	"github.com/dhanuka84/hybrid-ai-platform/internal/milvus"
	"github.com/dhanuka84/hybrid-ai-platform/internal/ollama"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

type Platform struct {
	Repository *postgres.Repository
	Vectors    *milvus.Store
	Service    *service.Service
}

func Open(ctx context.Context, cfg config.Config) (*Platform, error) {
	repository, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	vectors, err := milvus.Open(ctx, cfg.MilvusAddress, cfg.MilvusDatabase, cfg.MilvusAPIKey, cfg.MilvusCollection, cfg.EmbeddingDimension)
	if err != nil {
		repository.Close()
		return nil, err
	}
	embedder := ollama.New(cfg.OllamaURL, cfg.EmbeddingModel)
	artifactStore := artifacts.NewLocalStore(cfg.ArtifactsPath)
	return &Platform{
		Repository: repository,
		Vectors:    vectors,
		Service:    service.New(repository, artifactStore, embedder, vectors, cfg.SearchFallback, cfg.AutoApproveLocal),
	}, nil
}

func (p *Platform) Initialize(ctx context.Context) error {
	if err := p.Repository.Ping(ctx); err != nil {
		return fmt.Errorf("PostgreSQL unavailable: %w", err)
	}
	if err := p.Vectors.EnsureCollection(ctx); err != nil {
		return err
	}
	return nil
}

func (p *Platform) Close(ctx context.Context) error {
	p.Repository.Close()
	return p.Vectors.Close(ctx)
}
