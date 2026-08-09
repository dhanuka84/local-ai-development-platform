package platform

import (
	"context"
	"fmt"

	golanganalyzer "github.com/dhanuka84/hybrid-ai-platform/components/codegraph/golang"
	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/authorization"
	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/milvus"
	"github.com/dhanuka84/hybrid-ai-platform/internal/ollama"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

type Platform struct {
	Repository *postgres.Repository
	Vectors    *milvus.Store
	Service    *service.Service
	principals []domain.PrincipalBootstrap
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
	svc := service.New(repository, artifactStore, embedder, vectors, cfg.SearchFallback, cfg.AutoApproveLocal)
	var authorizer domain.Authorizer = authorization.Disabled{}
	reportAuthorizer := false
	if cfg.AuthorizationMode == "cerbos" {
		cerbosAuthorizer, err := authorization.NewCerbos(cfg.CerbosAddress, cfg.CerbosRequestTimeout)
		if err != nil {
			_ = vectors.Close(ctx)
			repository.Close()
			return nil, err
		}
		authorizer, reportAuthorizer = cerbosAuthorizer, true
	}
	if err := svc.ConfigureAuthorization(authorizer, reportAuthorizer); err != nil {
		_ = vectors.Close(ctx)
		repository.Close()
		return nil, err
	}
	if cfg.CodeGraphEnabled {
		if err := svc.ConfigureCodeGraph(golanganalyzer.New(), cfg.CodeGraphAllowedRoots, service.CodeGraphLimits{
			MaxFiles: cfg.CodeGraphMaxFiles, MaxEntities: cfg.CodeGraphMaxEntities, MaxRelations: cfg.CodeGraphMaxRelations,
		}); err != nil {
			_ = vectors.Close(ctx)
			repository.Close()
			return nil, fmt.Errorf("configure code graph: %w", err)
		}
	}
	return &Platform{
		Repository: repository,
		Vectors:    vectors,
		Service:    svc,
		principals: cfg.AuthPrincipals,
	}, nil
}

func (p *Platform) Initialize(ctx context.Context) error {
	if err := p.Repository.Ping(ctx); err != nil {
		return fmt.Errorf("PostgreSQL unavailable: %w", err)
	}
	if err := p.Repository.BootstrapPrincipals(ctx, p.principals); err != nil {
		return fmt.Errorf("bootstrap principals: %w", err)
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
