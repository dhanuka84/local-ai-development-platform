package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/authorization"
	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
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
		return errors.New("usage: admin <migrate|milvus-init|doctor|reindex|candidates|get|approve|reject> [arguments]")
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
		codeEntityCount, err := repository.RequeueCodeEntities(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("queued %d approved knowledge items, %d repository relations, and %d code entities for indexing\n", knowledgeCount, relationCount, codeEntityCount)
		return nil
	case "candidates":
		if len(args) > 3 {
			return errors.New("usage: admin candidates [project-id] [limit]")
		}
		projectID := ""
		limit := 25
		if len(args) >= 2 {
			projectID = args[1]
		}
		if len(args) == 3 {
			limit, err = strconv.Atoi(args[2])
			if err != nil || limit < 1 || limit > 100 {
				return errors.New("candidate limit must be an integer from 1 to 100")
			}
		}
		repository, err := openRepository(ctx, cfg)
		if err != nil {
			return err
		}
		defer repository.Close()
		items, err := repository.ListCandidates(ctx, projectID, limit)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			Count int         `json:"count"`
			Items interface{} `json:"items"`
		}{Count: len(items), Items: items})
	case "get":
		if len(args) != 2 {
			return errors.New("usage: admin get <knowledge-id>")
		}
		repository, err := openRepository(ctx, cfg)
		if err != nil {
			return err
		}
		defer repository.Close()
		item, err := repository.GetKnowledge(ctx, args[1], true)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(item)
	case "approve", "reject":
		if len(args) != 2 {
			return fmt.Errorf("usage: admin %s <knowledge-id>", args[0])
		}
		return decideCandidate(ctx, cfg, args[1], args[0])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func decideCandidate(ctx context.Context, cfg config.Config, id, action string) error {
	if cfg.AuthToken == "" {
		return errors.New("AUTH_TOKEN is required for an accountable local candidate decision")
	}
	repository, err := openRepository(ctx, cfg)
	if err != nil {
		return err
	}
	defer repository.Close()
	tokenHash := sha256.Sum256([]byte(cfg.AuthToken))
	principal, err := repository.AuthenticatePrincipal(ctx, tokenHash[:])
	if err != nil {
		return fmt.Errorf("authenticate Product Owner: %w", err)
	}
	candidate, err := repository.GetKnowledge(ctx, id, true)
	if err != nil {
		return err
	}
	workflowLinked, qaValidated := candidate.WorkflowID != "", false
	if workflowLinked {
		workflow, workflowErr := repository.GetWorkflow(ctx, candidate.WorkflowID)
		if workflowErr != nil {
			return workflowErr
		}
		qaValidated = workflow.QAValidatedBy != "" && workflow.State == "promotion_pending"
	}
	var authorizer domain.Authorizer = authorization.Disabled{}
	if cfg.AuthorizationMode == "cerbos" {
		authorizer, err = authorization.NewCerbos(cfg.CerbosAddress, cfg.CerbosRequestTimeout)
		if err != nil {
			return err
		}
	}
	decision, err := authorizer.Authorize(ctx, domain.AuthorizationRequest{
		Principal: principal, ResourceKind: "knowledge_candidate", ResourceID: candidate.ID, Action: action,
		Attributes: map[string]any{
			"project_id": candidate.ProjectID, "status": candidate.Status,
			"workflow_linked": workflowLinked, "qa_validated": qaValidated,
		},
	})
	if err != nil {
		return fmt.Errorf("authorization unavailable: %w", err)
	}
	if !decision.Allowed {
		return fmt.Errorf("Product Owner %q is not authorized to %s candidate %q", principal.ID, action, candidate.ID)
	}
	var item domain.KnowledgeItem
	if action == "approve" {
		item, err = repository.ApproveCandidate(ctx, candidate.ID, principal.ID)
	} else {
		item, err = repository.RejectCandidate(ctx, candidate.ID, principal.ID)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(item)
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
	if cfg.AuthorizationMode == "cerbos" {
		authorizer, err := authorization.NewCerbos(cfg.CerbosAddress, cfg.CerbosRequestTimeout)
		if err != nil {
			status["cerbos"] = err.Error()
		} else if err := authorizer.Ping(ctx); err != nil {
			status["cerbos"] = err.Error()
		} else {
			status["cerbos"] = "ok"
		}
	}
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
