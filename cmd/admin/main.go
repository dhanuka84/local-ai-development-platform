package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/age"
	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/authorization"
	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/dhanuka84/hybrid-ai-platform/internal/milvus"
	"github.com/dhanuka84/hybrid-ai-platform/internal/ollama"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
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
		return errors.New("usage: admin <migrate|age-rebuild|milvus-init|doctor|reindex|compact-code-outbox|repository-upsert|candidates|get|validate|approve|reject> [arguments]")
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
		if cfg.GraphBackend == "apache-age" {
			graphStore, err := age.New(repository.Pool(), repository, cfg.AgeGraphName)
			if err != nil {
				return err
			}
			if err := graphStore.Ensure(ctx); err != nil {
				return err
			}
		}
		if err := migrations.Apply(ctx, repository.Pool()); err != nil {
			return err
		}
		fmt.Println("PostgreSQL migrations applied")
		return nil
	case "age-rebuild":
		repository, err := openRepository(ctx, cfg)
		if err != nil {
			return err
		}
		defer repository.Close()
		graphStore, err := age.New(repository.Pool(), repository, cfg.AgeGraphName)
		if err != nil {
			return err
		}
		if err := graphStore.Ensure(ctx); err != nil {
			return err
		}
		stats, err := graphStore.Rebuild(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(stats)
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
		edgeCount, err := repository.RequeueSemanticGraphEdges(ctx)
		if err != nil {
			return err
		}
		definitionCount, err := repository.RequeueContextDefinitions(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("queued %d approved knowledge items, %d repository relations, %d code entities, %d semantic graph edges, and %d approved context definitions for indexing\n", knowledgeCount, relationCount, codeEntityCount, edgeCount, definitionCount)
		return nil
	case "repository-upsert":
		if len(args) != 6 {
			return errors.New("usage: admin repository-upsert <project-id> <name> <canonical-url> <default-branch> <revision>")
		}
		repository, err := openRepository(ctx, cfg)
		if err != nil {
			return err
		}
		defer repository.Close()
		item, err := repository.UpsertSoftwareRepository(ctx, args[1], domain.SoftwareRepository{
			Name: args[2], CanonicalURL: args[3], DefaultBranch: args[4], Revision: args[5],
		})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(item)
	case "compact-code-outbox":
		repository, err := openRepository(ctx, cfg)
		if err != nil {
			return err
		}
		defer repository.Close()
		count, err := repository.CompactCodeEntityOutbox(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			Completed int64 `json:"completed"`
		}{Completed: count})
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
		if len(args) != 6 {
			return fmt.Errorf("validation_required: usage: admin %s <knowledge-id> <expected-version> <validation-id-or-dash> <idempotency-key> <reason>", args[0])
		}
		version, err := strconv.Atoi(args[2])
		if err != nil {
			return err
		}
		validationID := args[3]
		if validationID == "-" {
			validationID = ""
		}
		return decideCandidate(ctx, cfg, service.DecisionInput{KnowledgeID: args[1], ExpectedVersion: version, ValidationID: validationID, Decision: args[0], IdempotencyKey: args[4], Reason: args[5]})
	case "validate":
		if len(args) != 4 {
			return errors.New("usage: admin validate <validation-input.json> <work-packet.json> <patch-file>")
		}
		return validateCandidate(ctx, cfg, args[1], args[2], args[3])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func governanceService(ctx context.Context, cfg config.Config) (*service.Service, context.Context, func(), error) {
	if cfg.AuthToken == "" {
		return nil, ctx, nil, errors.New("AUTH_TOKEN is required for an accountable local candidate decision")
	}
	repository, err := openRepository(ctx, cfg)
	if err != nil {
		return nil, ctx, nil, err
	}
	tokenHash := sha256.Sum256([]byte(cfg.AuthToken))
	principal, err := repository.AuthenticatePrincipal(ctx, tokenHash[:])
	if err != nil {
		repository.Close()
		return nil, ctx, nil, fmt.Errorf("authenticate operator: %w", err)
	}
	var authorizer domain.Authorizer = authorization.Disabled{}
	if cfg.AuthorizationMode == "cerbos" {
		authorizer, err = authorization.NewCerbos(cfg.CerbosAddress, cfg.CerbosRequestTimeout)
		if err != nil {
			repository.Close()
			return nil, ctx, nil, err
		}
	}
	svc := service.New(repository, artifacts.NewLocalStore(cfg.ArtifactsPath), nil, nil, false, false)
	svc.ConfigureSourceRoots(cfg.CodeGraphAllowedRoots)
	if err := svc.ConfigureAuthorization(authorizer, cfg.AuthorizationMode == "cerbos"); err != nil {
		repository.Close()
		return nil, ctx, nil, err
	}
	return svc, identity.WithPrincipal(ctx, principal), repository.Close, nil
}

func decideCandidate(ctx context.Context, cfg config.Config, input service.DecisionInput) error {
	svc, ctx, close, err := governanceService(ctx, cfg)
	if err != nil {
		return err
	}
	defer close()
	item, err := svc.DecideKnowledge(ctx, input)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(item)
}

func validateCandidate(ctx context.Context, cfg config.Config, inputPath, packetPath, patchPath string) error {
	var input service.ValidationInput
	var packet workpacket.Packet
	inputJSON, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}
	inputDecoder := json.NewDecoder(bytes.NewReader(inputJSON))
	inputDecoder.DisallowUnknownFields()
	if err := inputDecoder.Decode(&input); err != nil {
		return err
	}
	packetJSON, err := os.ReadFile(packetPath)
	if err != nil {
		return err
	}
	packetDecoder := json.NewDecoder(bytes.NewReader(packetJSON))
	packetDecoder.DisallowUnknownFields()
	if err := packetDecoder.Decode(&packet); err != nil {
		return err
	}
	patch, err := os.ReadFile(patchPath)
	if err != nil {
		return err
	}
	svc, ctx, close, err := governanceService(ctx, cfg)
	if err != nil {
		return err
	}
	defer close()
	report, err := svc.VerifyKnowledgePatch(ctx, input, packet, patch)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
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
		if cfg.GraphBackend == "apache-age" {
			graphStore, graphErr := age.New(repository.Pool(), repository, cfg.AgeGraphName)
			if graphErr != nil {
				status["apache-age"] = graphErr.Error()
			} else if graphErr := graphStore.Ping(ctx); graphErr != nil {
				status["apache-age"] = graphErr.Error()
			} else {
				status["apache-age"] = "ok"
			}
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
