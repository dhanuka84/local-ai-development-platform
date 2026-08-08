package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
	"github.com/dhanuka84/hybrid-ai-platform/internal/milvus"
	"github.com/dhanuka84/hybrid-ai-platform/internal/ollama"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
	"github.com/dhanuka84/hybrid-ai-platform/migrations"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "admin:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: admin <migrate|milvus-init|doctor|reindex|approve|reject> [arguments]")
	}
	cfg, err := config.LoadCLI()
	if err != nil {
		return err
	}
	switch args[0] {
	case "migrate":
		repository, err := postgres.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer repository.Close()
		if err := migrations.Apply(ctx, repository.Pool()); err != nil {
			return err
		}
		fmt.Println("PostgreSQL migrations applied")
		return nil
	case "milvus-init":
		store, err := milvus.Open(ctx, cfg.MilvusAddress, cfg.MilvusDatabase, cfg.MilvusAPIKey, cfg.MilvusCollection, cfg.EmbeddingDimension)
		if err != nil {
			return err
		}
		defer func() { _ = store.Close(context.Background()) }()
		if err := store.EnsureCollection(ctx); err != nil {
			return err
		}
		fmt.Println("Milvus collection is ready:", cfg.MilvusCollection)
		return nil
	case "doctor":
		return doctor(ctx, cfg)
	case "reindex":
		repository, err := openRepository(ctx, cfg)
		if err != nil {
			return err
		}
		defer repository.Close()
		knowledgeCount, err := repository.RequeueApprovedKnowledge(ctx)
		if err != nil {
			return err
		}
		relationCount, err := repository.RequeueRepositoryRelations(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("queued %d approved knowledge items and %d repository relations for indexing\n", knowledgeCount, relationCount)
		return nil
	case "approve", "reject":
		if len(args) != 3 {
			return fmt.Errorf("usage: admin %s <knowledge-id> <actor>", args[0])
		}
		repository, err := openRepository(ctx, cfg)
		if err != nil {
			return err
		}
		defer repository.Close()
		var item any
		if args[0] == "approve" {
			item, err = repository.ApproveCandidate(ctx, args[1], args[2])
		} else {
			item, err = repository.RejectCandidate(ctx, args[1], args[2])
		}
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(item)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func openRepository(ctx context.Context, cfg config.Config) (*postgres.Repository, error) {
	repository, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := repository.Ping(ctx); err != nil {
		repository.Close()
		return nil, err
	}
	return repository, nil
}

func doctor(ctx context.Context, cfg config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status := make(map[string]string)
	repository, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		status["postgres"] = err.Error()
	} else {
		defer repository.Close()
		if err := repository.Ping(ctx); err != nil {
			status["postgres"] = err.Error()
		} else {
			status["postgres"] = "ok"
		}
	}
	if err := ollama.New(cfg.OllamaURL, cfg.EmbeddingModel).Ping(ctx); err != nil {
		status["ollama"] = err.Error()
	} else {
		status["ollama"] = "ok"
	}
	store, err := milvus.Open(ctx, cfg.MilvusAddress, cfg.MilvusDatabase, cfg.MilvusAPIKey, cfg.MilvusCollection, cfg.EmbeddingDimension)
	if err != nil {
		status["milvus"] = err.Error()
	} else {
		defer func() { _ = store.Close(context.Background()) }()
		if err := store.Ping(ctx); err != nil {
			status["milvus"] = err.Error()
		} else {
			status["milvus"] = "ok"
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(status)
	for _, value := range status {
		if value != "ok" {
			return errors.New("one or more dependencies are unavailable")
		}
	}
	return nil
}
