package config

import "testing"

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
