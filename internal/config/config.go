package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment        string
	LogLevel           string
	HTTPAddress        string
	MCPTransport       string
	AuthMode           string
	AuthToken          string
	DatabaseURL        string
	ArtifactsPath      string
	OllamaURL          string
	EmbeddingModel     string
	EmbeddingDimension int
	MilvusAddress      string
	MilvusDatabase     string
	MilvusCollection   string
	MilvusAPIKey       string
	WorkerPollInterval time.Duration
	WorkerBatchSize    int
	SearchFallback     bool
	AutoApproveLocal   bool
}

func Load() (Config, error) { return load(true) }

func LoadCLI() (Config, error) { return load(false) }

func load(validateAuth bool) (Config, error) {
	cfg := Config{
		Environment:        env("APP_ENV", "local"),
		LogLevel:           env("LOG_LEVEL", "info"),
		HTTPAddress:        env("HTTP_ADDRESS", "127.0.0.1:8080"),
		MCPTransport:       env("MCP_TRANSPORT", "http"),
		AuthMode:           env("AUTH_MODE", "token"),
		AuthToken:          os.Getenv("AUTH_TOKEN"),
		DatabaseURL:        env("DATABASE_URL", "postgres://hybrid:hybrid@127.0.0.1:5432/hybrid?sslmode=disable"),
		ArtifactsPath:      env("ARTIFACTS_PATH", "./data/artifacts"),
		OllamaURL:          strings.TrimRight(env("OLLAMA_URL", "http://127.0.0.1:11434"), "/"),
		EmbeddingModel:     env("OLLAMA_EMBEDDING_MODEL", "embeddinggemma"),
		MilvusAddress:      env("MILVUS_ADDRESS", "127.0.0.1:19530"),
		MilvusDatabase:     env("MILVUS_DATABASE", "default"),
		MilvusCollection:   env("MILVUS_COLLECTION", "approved_knowledge_v1"),
		MilvusAPIKey:       os.Getenv("MILVUS_API_KEY"),
		WorkerPollInterval: durationEnv("WORKER_POLL_INTERVAL", 2*time.Second),
		EmbeddingDimension: intEnv("EMBEDDING_DIMENSION", 768),
		WorkerBatchSize:    intEnv("WORKER_BATCH_SIZE", 25),
		SearchFallback:     boolEnv("SEARCH_LEXICAL_FALLBACK", true),
		AutoApproveLocal:   boolEnv("AUTO_APPROVE_LOCAL", false),
	}

	switch cfg.MCPTransport {
	case "http", "stdio":
	default:
		return Config{}, fmt.Errorf("MCP_TRANSPORT must be http or stdio, got %q", cfg.MCPTransport)
	}
	switch cfg.AuthMode {
	case "token":
		if validateAuth && cfg.MCPTransport == "http" && strings.TrimSpace(cfg.AuthToken) == "" {
			return Config{}, errors.New("AUTH_TOKEN is required for HTTP transport when AUTH_MODE=token")
		}
	case "none":
		if cfg.Environment != "local" {
			return Config{}, errors.New("AUTH_MODE=none is allowed only when APP_ENV=local")
		}
	default:
		return Config{}, fmt.Errorf("AUTH_MODE must be token or none, got %q", cfg.AuthMode)
	}
	if cfg.EmbeddingDimension < 1 || cfg.WorkerBatchSize < 1 {
		return Config{}, errors.New("EMBEDDING_DIMENSION and WORKER_BATCH_SIZE must be positive")
	}
	return cfg, nil
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
