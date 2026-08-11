package platform

import (
	"context"
	"fmt"
	"log/slog"

	golanganalyzer "github.com/dhanuka84/hybrid-ai-platform/components/codegraph/golang"
	codegraphrouter "github.com/dhanuka84/hybrid-ai-platform/components/codegraph/router"
	scipanalyzer "github.com/dhanuka84/hybrid-ai-platform/components/codegraph/scip"
	"github.com/dhanuka84/hybrid-ai-platform/internal/age"
	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/authorization"
	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	graphfallback "github.com/dhanuka84/hybrid-ai-platform/internal/graph"
	"github.com/dhanuka84/hybrid-ai-platform/internal/graphrag"
	"github.com/dhanuka84/hybrid-ai-platform/internal/milvus"
	"github.com/dhanuka84/hybrid-ai-platform/internal/ollama"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

type Platform struct {
	Repository    *postgres.Repository
	Vectors       *milvus.Store
	Service       *service.Service
	Graphs        domain.GraphStore
	Projector     domain.GraphProjector
	graphHealth   domain.HealthChecker
	graphRequired bool
	principals    []domain.PrincipalBootstrap
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
	relationalGraphs := postgres.NewRecursiveGraphStore(repository)
	var graphStore domain.GraphStore = relationalGraphs
	var graphHealth domain.HealthChecker
	var projector domain.GraphProjector
	if cfg.GraphBackend == "apache-age" {
		ageStore, err := age.New(repository.Pool(), repository, cfg.AgeGraphName)
		if err != nil {
			_ = vectors.Close(ctx)
			repository.Close()
			return nil, fmt.Errorf("configure Apache AGE: %w", err)
		}
		graphStore, graphHealth, projector = ageStore, ageStore, ageStore
		if cfg.GraphFallbackEnabled {
			graphStore = graphfallback.WithFallback(ageStore, relationalGraphs, slog.Default())
		}
	}
	graphRAG, err := graphrag.New(repository, embedder, vectors, graphStore)
	if err != nil {
		_ = vectors.Close(ctx)
		repository.Close()
		return nil, err
	}
	if err := svc.ConfigureGraphs(graphStore, graphHealth, graphRAG); err != nil {
		_ = vectors.Close(ctx)
		repository.Close()
		return nil, err
	}
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
		jvmAnalyzer, err := scipanalyzer.New(scipanalyzer.Config{
			Name: "scip-java", Version: "0.13.1", Command: cfg.CodeGraphJVMIndexer, Language: "jvm",
		})
		if err != nil {
			_ = vectors.Close(ctx)
			repository.Close()
			return nil, fmt.Errorf("configure JVM code analyzer: %w", err)
		}
		typeScriptAnalyzer, err := scipanalyzer.New(scipanalyzer.Config{
			Name: "scip-typescript", Version: "0.4.0", Command: cfg.CodeGraphTSIndexer, Language: "typescript",
		})
		if err != nil {
			_ = vectors.Close(ctx)
			repository.Close()
			return nil, fmt.Errorf("configure TypeScript code analyzer: %w", err)
		}
		pythonAnalyzer, err := scipanalyzer.New(scipanalyzer.Config{
			Name: "scip-python", Version: "0.6.6", Command: cfg.CodeGraphPythonIndexer, Language: "python",
		})
		if err != nil {
			_ = vectors.Close(ctx)
			repository.Close()
			return nil, fmt.Errorf("configure Python code analyzer: %w", err)
		}
		analyzer, err := codegraphrouter.New(
			codegraphrouter.Candidate{Analyzer: golanganalyzer.New(), Extensions: []string{".go"}, Markers: []string{"go.mod"}},
			codegraphrouter.Candidate{
				Analyzer: jvmAnalyzer, Extensions: []string{".java", ".kt"},
				Markers: []string{"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradlew"},
			},
			codegraphrouter.Candidate{
				Analyzer: typeScriptAnalyzer, Extensions: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
				Markers: []string{"package.json", "tsconfig.json"},
			},
			codegraphrouter.Candidate{Analyzer: pythonAnalyzer, Extensions: []string{".py"}},
		)
		if err != nil {
			_ = vectors.Close(ctx)
			repository.Close()
			return nil, fmt.Errorf("configure code analyzer routing: %w", err)
		}
		if err := svc.ConfigureCodeGraph(analyzer, cfg.CodeGraphAllowedRoots, service.CodeGraphLimits{
			MaxFiles: cfg.CodeGraphMaxFiles, MaxEntities: cfg.CodeGraphMaxEntities, MaxRelations: cfg.CodeGraphMaxRelations,
		}); err != nil {
			_ = vectors.Close(ctx)
			repository.Close()
			return nil, fmt.Errorf("configure code graph: %w", err)
		}
	}
	return &Platform{
		Repository:    repository,
		Vectors:       vectors,
		Service:       svc,
		Graphs:        graphStore,
		Projector:     projector,
		graphHealth:   graphHealth,
		graphRequired: cfg.GraphBackend == "apache-age" && !cfg.GraphFallbackEnabled,
		principals:    cfg.AuthPrincipals,
	}, nil
}

func (p *Platform) Initialize(ctx context.Context) error {
	if err := p.Repository.Ping(ctx); err != nil {
		return fmt.Errorf("PostgreSQL unavailable: %w", err)
	}
	if p.graphHealth != nil {
		if err := p.graphHealth.Ping(ctx); err != nil {
			if p.graphRequired {
				return fmt.Errorf("Apache AGE unavailable: %w", err)
			}
			slog.Warn("Apache AGE unavailable; PostgreSQL graph fallback remains active", "error", err)
		}
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
