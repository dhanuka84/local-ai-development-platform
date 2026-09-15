package domain

import (
	"slices"
	"testing"
)

func TestPrincipalProjectsHaveStableOrderAndIndependentStorage(t *testing.T) {
	principal := Principal{RoleBindings: map[string][]string{
		"zeta":  {"development"},
		"*":     {"qa"},
		"alpha": {"operations"},
	}}
	projects := principal.ProjectIDs()
	if want := []string{"*", "alpha", "zeta"}; !slices.Equal(projects, want) {
		t.Fatalf("ProjectIDs() = %q; want %q", projects, want)
	}
	projects[0] = "changed"
	if _, ok := principal.RoleBindings["*"]; !ok {
		t.Fatal("changing returned projects mutated role bindings")
	}
	if got := (Principal{}).ProjectIDs(); got == nil || len(got) != 0 {
		t.Fatalf("ProjectIDs() without bindings = %#v; want non-nil empty slice", got)
	}
}
