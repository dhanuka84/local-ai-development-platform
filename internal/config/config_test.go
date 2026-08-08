package config

import (
	"path/filepath"
	"testing"
)

func TestLoadRequiresTokenForHTTP(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("MCP_TRANSPORT", "http")
	t.Setenv("AUTH_MODE", "token")
	t.Setenv("AUTH_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded without an HTTP authentication token")
	}
}

func TestLoadAllowsUnauthenticatedLocalStdio(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("MCP_TRANSPORT", "stdio")
	t.Setenv("AUTH_MODE", "none")
	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsUnauthenticatedEnterprise(t *testing.T) {
	t.Setenv("APP_ENV", "enterprise")
	t.Setenv("AUTH_MODE", "none")
	if _, err := Load(); err == nil {
		t.Fatal("Load() allowed AUTH_MODE=none outside local mode")
	}
}

func TestLoadParsesCodeGraphRootsAndLimits(t *testing.T) {
	t.Setenv("CODEGRAPH_ALLOWED_ROOTS", "/srv/repo-a"+string(filepath.ListSeparator)+"/srv/repo-b")
	t.Setenv("CODEGRAPH_MAX_FILES", "123")
	t.Setenv("CODEGRAPH_MAX_ENTITIES", "456")
	t.Setenv("CODEGRAPH_MAX_RELATIONS", "789")
	cfg, err := LoadCLI()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CodeGraphAllowedRoots) != 2 || cfg.CodeGraphAllowedRoots[1] != "/srv/repo-b" ||
		cfg.CodeGraphMaxFiles != 123 || cfg.CodeGraphMaxEntities != 456 || cfg.CodeGraphMaxRelations != 789 {
		t.Fatalf("code graph config = %#v", cfg)
	}
}

func TestLoadDisablesSynchronousCodeAnalysisByDefaultInEnterprise(t *testing.T) {
	t.Setenv("APP_ENV", "enterprise")
	cfg, err := LoadCLI()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CodeGraphEnabled {
		t.Fatal("enterprise mode enabled synchronous filesystem analysis")
	}
}
