package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type Config struct {
	Environment            string
	LogLevel               string
	HTTPAddress            string
	MCPTransport           string
	AuthMode               string
	AuthToken              string
	AuthPrincipals         []domain.PrincipalBootstrap
	AuthorizationMode      string
	CerbosAddress          string
	CerbosRequestTimeout   time.Duration
	DatabaseURL            string
	ArtifactsPath          string
	OllamaURL              string
	EmbeddingModel         string
	EmbeddingDimension     int
	MilvusAddress          string
	MilvusDatabase         string
	MilvusCollection       string
	MilvusAPIKey           string
	WorkerPollInterval     time.Duration
	WorkerBatchSize        int
	SearchFallback         bool
	AutoApproveLocal       bool
	CodeGraphEnabled       bool
	CodeGraphAllowedRoots  []string
	CodeGraphMaxFiles      int
	CodeGraphMaxEntities   int
	CodeGraphMaxRelations  int
	CodeGraphJVMIndexer    string
	CodeGraphTSIndexer     string
	CodeGraphPythonIndexer string
}

func Load() (Config, error) { return load(true) }

func LoadCLI() (Config, error) { return load(false) }

func load(validateAuth bool) (Config, error) {
	environment := env("APP_ENV", "local")
	authorizationDefault := "cerbos"
	if environment == "local" {
		authorizationDefault = "none"
	}
	authPrincipals, err := principalBootstraps()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Environment:            environment,
		LogLevel:               env("LOG_LEVEL", "info"),
		HTTPAddress:            env("HTTP_ADDRESS", "127.0.0.1:8080"),
		MCPTransport:           env("MCP_TRANSPORT", "http"),
		AuthMode:               env("AUTH_MODE", "token"),
		AuthToken:              os.Getenv("AUTH_TOKEN"),
		AuthPrincipals:         authPrincipals,
		AuthorizationMode:      env("AUTHORIZATION_MODE", authorizationDefault),
		CerbosAddress:          env("CERBOS_ADDRESS", "127.0.0.1:3593"),
		CerbosRequestTimeout:   durationEnv("CERBOS_REQUEST_TIMEOUT", 2*time.Second),
		DatabaseURL:            env("DATABASE_URL", "postgres://hybrid:hybrid@127.0.0.1:5432/hybrid?sslmode=disable"),
		ArtifactsPath:          env("ARTIFACTS_PATH", "./data/artifacts"),
		OllamaURL:              strings.TrimRight(env("OLLAMA_URL", "http://127.0.0.1:11434"), "/"),
		EmbeddingModel:         env("OLLAMA_EMBEDDING_MODEL", "embeddinggemma"),
		MilvusAddress:          env("MILVUS_ADDRESS", "127.0.0.1:19530"),
		MilvusDatabase:         env("MILVUS_DATABASE", "default"),
		MilvusCollection:       env("MILVUS_COLLECTION", "approved_knowledge_v1"),
		MilvusAPIKey:           os.Getenv("MILVUS_API_KEY"),
		WorkerPollInterval:     durationEnv("WORKER_POLL_INTERVAL", 2*time.Second),
		EmbeddingDimension:     intEnv("EMBEDDING_DIMENSION", 768),
		WorkerBatchSize:        intEnv("WORKER_BATCH_SIZE", 25),
		SearchFallback:         boolEnv("SEARCH_LEXICAL_FALLBACK", true),
		AutoApproveLocal:       boolEnv("AUTO_APPROVE_LOCAL", false),
		CodeGraphEnabled:       boolEnv("CODEGRAPH_ENABLED", environment == "local"),
		CodeGraphAllowedRoots:  pathListEnv("CODEGRAPH_ALLOWED_ROOTS", "."),
		CodeGraphMaxFiles:      intEnv("CODEGRAPH_MAX_FILES", 5000),
		CodeGraphMaxEntities:   intEnv("CODEGRAPH_MAX_ENTITIES", 200000),
		CodeGraphMaxRelations:  intEnv("CODEGRAPH_MAX_RELATIONS", 1000000),
		CodeGraphJVMIndexer:    env("CODEGRAPH_JVM_INDEXER_COMMAND", "/usr/local/bin/hybrid-index-jvm"),
		CodeGraphTSIndexer:     env("CODEGRAPH_TYPESCRIPT_INDEXER_COMMAND", "/usr/local/bin/hybrid-index-typescript"),
		CodeGraphPythonIndexer: env("CODEGRAPH_PYTHON_INDEXER_COMMAND", "/usr/local/bin/hybrid-index-python"),
	}

	switch cfg.MCPTransport {
	case "http", "stdio":
	default:
		return Config{}, fmt.Errorf("MCP_TRANSPORT must be http or stdio, got %q", cfg.MCPTransport)
	}
	switch cfg.AuthMode {
	case "token":
		if validateAuth && cfg.MCPTransport == "http" && len(cfg.AuthPrincipals) == 0 {
			return Config{}, errors.New("AUTH_TOKEN is required for HTTP transport when AUTH_MODE=token")
		}
	case "none":
		if cfg.Environment != "local" {
			return Config{}, errors.New("AUTH_MODE=none is allowed only when APP_ENV=local")
		}
	default:
		return Config{}, fmt.Errorf("AUTH_MODE must be token or none, got %q", cfg.AuthMode)
	}
	if cfg.AuthMode == "none" && len(cfg.AuthPrincipals) == 0 {
		cfg.AuthPrincipals = []domain.PrincipalBootstrap{{
			ID: "human:local-developer", DisplayName: "Local developer", Human: true,
			Roles: []string{"development", "qa", "product_owner", "operations"}, ProjectIDs: []string{"*"},
		}}
	}
	switch cfg.AuthorizationMode {
	case "cerbos":
		if strings.TrimSpace(cfg.CerbosAddress) == "" {
			return Config{}, errors.New("CERBOS_ADDRESS is required when AUTHORIZATION_MODE=cerbos")
		}
	case "none":
		if cfg.Environment != "local" {
			return Config{}, errors.New("AUTHORIZATION_MODE=none is allowed only when APP_ENV=local")
		}
	default:
		return Config{}, fmt.Errorf("AUTHORIZATION_MODE must be cerbos or none, got %q", cfg.AuthorizationMode)
	}
	if cfg.EmbeddingDimension < 1 || cfg.WorkerBatchSize < 1 || (cfg.CodeGraphEnabled &&
		(cfg.CodeGraphMaxFiles < 1 || cfg.CodeGraphMaxEntities < 1 || cfg.CodeGraphMaxRelations < 1)) || cfg.CerbosRequestTimeout <= 0 {
		return Config{}, errors.New("embedding, worker, and code-graph limits must be positive")
	}
	return cfg, nil
}

type principalBootstrapJSON struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Token       string   `json:"token"`
	Human       bool     `json:"human"`
	Roles       []string `json:"roles"`
	ProjectIDs  []string `json:"project_ids"`
}

func principalBootstraps() ([]domain.PrincipalBootstrap, error) {
	if raw := strings.TrimSpace(os.Getenv("AUTH_PRINCIPALS_JSON")); raw != "" {
		var definitions []principalBootstrapJSON
		if err := json.Unmarshal([]byte(raw), &definitions); err != nil {
			return nil, fmt.Errorf("parse AUTH_PRINCIPALS_JSON: %w", err)
		}
		result := make([]domain.PrincipalBootstrap, 0, len(definitions))
		for index, definition := range definitions {
			principal := domain.PrincipalBootstrap{
				ID: strings.TrimSpace(definition.ID), DisplayName: strings.TrimSpace(definition.DisplayName),
				Token: strings.TrimSpace(definition.Token), Human: definition.Human,
				Roles: cleanStrings(definition.Roles), ProjectIDs: cleanStrings(definition.ProjectIDs),
			}
			if principal.ID == "" || principal.Token == "" || len(principal.Roles) == 0 || len(principal.ProjectIDs) == 0 {
				return nil, fmt.Errorf("AUTH_PRINCIPALS_JSON entry %d requires id, token, roles, and project_ids", index)
			}
			result = append(result, principal)
		}
		return validatePrincipalBootstraps(result)
	}
	result := make([]domain.PrincipalBootstrap, 0, 2)
	token := strings.TrimSpace(os.Getenv("AUTH_TOKEN"))
	if token != "" {
		result = append(result, domain.PrincipalBootstrap{
			ID:          env("AUTH_PRINCIPAL_ID", "human:local-developer"),
			DisplayName: env("AUTH_PRINCIPAL_DISPLAY_NAME", "Local developer"),
			Token:       token,
			Human:       true,
			Roles:       commaListEnv("AUTH_PRINCIPAL_ROLES", "development,qa,product_owner,operations"),
			ProjectIDs:  commaListEnv("AUTH_PRINCIPAL_PROJECTS", "*"),
		})
	}
	controllerToken := strings.TrimSpace(os.Getenv("CONTROLLER_AUTH_TOKEN"))
	if controllerToken != "" {
		if controllerToken == token {
			return nil, errors.New("CONTROLLER_AUTH_TOKEN must differ from AUTH_TOKEN")
		}
		result = append(result, domain.PrincipalBootstrap{
			ID: "agent:openclaw-controller", DisplayName: "OpenClaw workflow controller", Token: controllerToken,
			Human: false, Roles: []string{"controller"}, ProjectIDs: commaListEnv("CONTROLLER_AUTH_PROJECTS", "*"),
		})
	}
	return validatePrincipalBootstraps(result)
}

func validatePrincipalBootstraps(principals []domain.PrincipalBootstrap) ([]domain.PrincipalBootstrap, error) {
	allowedRoles := map[string]struct{}{
		"controller": {}, "development": {}, "qa": {}, "product_owner": {}, "operations": {},
		"cloud_reviewer": {}, "maintenance_executor": {}, "repository_analyzer": {},
	}
	ids := make(map[string]struct{}, len(principals))
	tokens := make(map[string]struct{}, len(principals))
	for _, principal := range principals {
		if _, exists := ids[principal.ID]; exists {
			return nil, fmt.Errorf("duplicate principal id %q", principal.ID)
		}
		ids[principal.ID] = struct{}{}
		if principal.Token != "" {
			if _, exists := tokens[principal.Token]; exists {
				return nil, errors.New("principal tokens must be unique")
			}
			tokens[principal.Token] = struct{}{}
		}
		for _, role := range principal.Roles {
			if _, ok := allowedRoles[role]; !ok {
				return nil, fmt.Errorf("principal %q has unsupported role %q", principal.ID, role)
			}
		}
	}
	return principals, nil
}

func commaListEnv(name, fallback string) []string {
	return cleanStrings(strings.Split(env(name, fallback), ","))
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func pathListEnv(name, fallback string) []string {
	value := env(name, fallback)
	result := make([]string, 0)
	for _, entry := range filepath.SplitList(value) {
		if entry = strings.TrimSpace(entry); entry != "" {
			result = append(result, entry)
		}
	}
	return result
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func intEnv(name string, fallback int) int {
	value, err := strconv.Atoi(env(name, strconv.Itoa(fallback)))
	if err != nil {
		return fallback
	}
	return value
}

func boolEnv(name string, fallback bool) bool {
	value, err := strconv.ParseBool(env(name, strconv.FormatBool(fallback)))
	if err != nil {
		return fallback
	}
	return value
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(env(name, fallback.String()))
	if err != nil {
		return fallback
	}
	return value
}
