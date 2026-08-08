// SPDX-License-Identifier: MPL-2.0

package golanganalyzer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
)

func TestAnalyzeBuildsDefinitionsCallsTestsAndImplementations(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module example.test/codegraphfixture\n\ngo 1.25\n")
	writeFixture(t, root, "service/service.go", `package service

// Store persists application values.
type Store interface { Save(string) error }
type Memory struct{}
func (Memory) Save(value string) error { return nil }
func Create(store Store) error { return store.Save("item") }
func Recursive(value int) int { if value == 0 { return 0 }; return Recursive(value - 1) }
`)
	writeFixture(t, root, "service/service_test.go", `package service

import "testing"
func TestCreate(t *testing.T) { _ = Create(Memory{}) }
`)
	initRepository(t, root)

	snapshot, err := New().Analyze(context.Background(), codegraph.Request{
		RepositoryPath: root, MaxFiles: 20, MaxEntities: 100, MaxRelations: 500,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := assertEntity(t, snapshot, "go:interface:example.test/codegraphfixture/service.Store")
	if store.Metadata["documentation"] != "Store persists application values." {
		t.Fatalf("Store documentation = %q", store.Metadata["documentation"])
	}
	assertEntity(t, snapshot, "go:type:example.test/codegraphfixture/service.Memory")
	assertEntity(t, snapshot, "go:method:example.test/codegraphfixture/service.Memory.Save")
	assertEntity(t, snapshot, "go:function:example.test/codegraphfixture/service.Create")
	assertEntity(t, snapshot, "go:test:example.test/codegraphfixture/service.TestCreate")
	assertRelation(t, snapshot, codegraph.RelationImplements,
		"go:type:example.test/codegraphfixture/service.Memory",
		"go:interface:example.test/codegraphfixture/service.Store")
	assertRelation(t, snapshot, codegraph.RelationCalls,
		"go:function:example.test/codegraphfixture/service.Create",
		"go:method:example.test/codegraphfixture/service.Store.Save")
	assertRelation(t, snapshot, codegraph.RelationCalls,
		"go:function:example.test/codegraphfixture/service.Recursive",
		"go:function:example.test/codegraphfixture/service.Recursive")
	assertRelation(t, snapshot, codegraph.RelationTests,
		"go:test:example.test/codegraphfixture/service.TestCreate",
		"go:function:example.test/codegraphfixture/service.Create")
	if snapshot.Statistics["files"] != 2 {
		t.Fatalf("file count = %d", snapshot.Statistics["files"])
	}
}

func TestAnalyzeEnforcesFileLimit(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module example.test/limit\n\ngo 1.25\n")
	writeFixture(t, root, "one.go", "package limit\n")
	writeFixture(t, root, "two.go", "package limit\n")
	initRepository(t, root)
	_, err := New().Analyze(context.Background(), codegraph.Request{RepositoryPath: root, MaxFiles: 1})
	if err == nil {
		t.Fatal("expected file limit error")
	}
}

func TestAnalyzeRejectsRevisionMismatchAndDirtyWorktree(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module example.test/revision\n\ngo 1.25\n")
	writeFixture(t, root, "revision.go", "package revision\n")
	initRepository(t, root)
	if _, err := New().Analyze(context.Background(), codegraph.Request{RepositoryPath: root, Revision: "not-head"}); err == nil {
		t.Fatal("expected revision mismatch")
	}
	writeFixture(t, root, "revision.go", "package revision\n\nconst Dirty = true\n")
	if _, err := New().Analyze(context.Background(), codegraph.Request{RepositoryPath: root}); err == nil {
		t.Fatal("expected dirty-worktree rejection")
	}
	snapshot, err := New().Analyze(context.Background(), codegraph.Request{RepositoryPath: root, AllowDirty: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.Revision, "+worktree.") {
		t.Fatalf("dirty revision = %q", snapshot.Revision)
	}
}

func assertEntity(t *testing.T, snapshot codegraph.Snapshot, key string) codegraph.Entity {
	t.Helper()
	for _, entity := range snapshot.Entities {
		if entity.Key == key {
			return entity
		}
	}
	t.Fatalf("entity %q not found; entities=%#v", key, snapshot.Entities)
	return codegraph.Entity{}
}

func assertRelation(t *testing.T, snapshot codegraph.Snapshot, kind codegraph.RelationKind, source, target string) {
	t.Helper()
	for _, relation := range snapshot.Relations {
		if relation.Kind == kind && relation.SourceKey == source && relation.TargetKey == target {
			return
		}
	}
	t.Fatalf("relation %s %s -> %s not found", kind, source, target)
}

func writeFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func initRepository(t *testing.T, root string) {
	t.Helper()
	commands := [][]string{
		{"git", "init", "--quiet", root},
		{"git", "-C", root, "add", "."},
		{"git", "-C", root, "-c", "user.name=Analyzer Test", "-c", "user.email=analyzer@example.test", "commit", "--quiet", "-m", "fixture"},
	}
	for _, arguments := range commands {
		command := exec.Command(arguments[0], arguments[1:]...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", arguments, err, output)
		}
	}
}
