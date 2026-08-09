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
	t.Setenv("AUTH_PRINCIPALS_JSON", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded without an HTTP authentication token")
	}
}

func TestLoadParsesMultiplePrincipals(t *testing.T) {
	t.Setenv("AUTH_PRINCIPALS_JSON", `[
      {"id":"agent:controller","token":"controller-secret","human":false,
       "roles":["controller"],"project_ids":["project-a"]},
      {"id":"human:alice","token":"alice-secret","human":true,
       "roles":["development","qa","product_owner","operations"],"project_ids":["project-a"]}
    ]`)
	cfg, err := LoadCLI()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AuthPrincipals) != 2 || cfg.AuthPrincipals[0].ID != "agent:controller" || cfg.AuthPrincipals[1].Roles[2] != "product_owner" {
		t.Fatalf("principals = %#v", cfg.AuthPrincipals)
	}
}

func TestLegacyLocalDefaultsToHumanAllRolesAndSeparateController(t *testing.T) {
	t.Setenv("AUTH_PRINCIPALS_JSON", "")
	t.Setenv("AUTH_TOKEN", "human-secret")
	t.Setenv("CONTROLLER_AUTH_TOKEN", "controller-secret")
	cfg, err := LoadCLI()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AuthPrincipals) != 2 {
		t.Fatalf("principals = %#v", cfg.AuthPrincipals)
	}
	human, controller := cfg.AuthPrincipals[0], cfg.AuthPrincipals[1]
	if !human.Human || human.ID != "human:local-developer" || len(human.Roles) != 4 || human.Roles[3] != "operations" {
		t.Fatalf("human principal = %#v", human)
	}
	if controller.Human || controller.ID != "agent:openclaw-controller" || len(controller.Roles) != 1 || controller.Roles[0] != "controller" {
		t.Fatalf("controller principal = %#v", controller)
	}
}

func TestControllerCannotReuseHumanCredential(t *testing.T) {
	t.Setenv("AUTH_PRINCIPALS_JSON", "")
	t.Setenv("AUTH_TOKEN", "shared-secret")
	t.Setenv("CONTROLLER_AUTH_TOKEN", "shared-secret")
	if _, err := LoadCLI(); err == nil {
		t.Fatal("LoadCLI() accepted one token for both human and workload identities")
	}
}

func TestPrincipalDefinitionsRejectDuplicateTokensAndUnknownRoles(t *testing.T) {
	t.Setenv("AUTH_PRINCIPALS_JSON", `[
      {"id":"one","token":"same","roles":["controller"],"project_ids":["project"]},
      {"id":"two","token":"same","roles":["controller"],"project_ids":["project"]}
    ]`)
	if _, err := LoadCLI(); err == nil {
		t.Fatal("LoadCLI() accepted a shared principal token")
	}
	t.Setenv("AUTH_PRINCIPALS_JSON", `[
      {"id":"one","token":"unique","roles":["administrator"],"project_ids":["project"]}
    ]`)
	if _, err := LoadCLI(); err == nil {
		t.Fatal("LoadCLI() accepted an unsupported role")
	}
}

func TestLoadRejectsAuthorizationDisabledOutsideLocal(t *testing.T) {
	t.Setenv("APP_ENV", "enterprise")
	t.Setenv("AUTHORIZATION_MODE", "none")
	if _, err := LoadCLI(); err == nil {
		t.Fatal("LoadCLI() allowed authorization to be disabled outside local mode")
	}
}

func TestLoadAllowsUnauthenticatedLocalStdio(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("MCP_TRANSPORT", "stdio")
	t.Setenv("AUTH_MODE", "none")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.AuthPrincipals) != 1 || !cfg.AuthPrincipals[0].Human || len(cfg.AuthPrincipals[0].Roles) != 4 {
		t.Fatalf("local auth-none principal = %#v", cfg.AuthPrincipals)
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
