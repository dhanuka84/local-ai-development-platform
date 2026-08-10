// SPDX-License-Identifier: MPL-2.0

package router

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
)

type fixtureAnalyzer struct {
	name     string
	language string
	calls    int
}

func (a *fixtureAnalyzer) Name() string    { return a.name }
func (a *fixtureAnalyzer) Version() string { return "test" }
func (a *fixtureAnalyzer) Analyze(_ context.Context, request codegraph.Request) (codegraph.Snapshot, error) {
	a.calls++
	repositoryKey := codegraph.StableKey(a.language, codegraph.EntityRepository, ".")
	return codegraph.Snapshot{
		RepositoryPath: request.RepositoryPath, RepositoryName: request.RepositoryName,
		Branch: request.Branch, Revision: request.Revision,
		Analyzer: a.Name(), AnalyzerVersion: a.Version(), StartedAt: time.Now(), CompletedAt: time.Now(),
		Entities: []codegraph.Entity{{
			Key: repositoryKey, Language: a.language, Kind: codegraph.EntityRepository,
			Name: "fixture", QualifiedName: ".",
		}},
	}, nil
}

func TestRouterCombinesDetectedLanguagesAndIgnoresDependencies(t *testing.T) {
	root := t.TempDir()
	writeRouterFixture(t, root, "main.go")
	writeRouterFixture(t, root, "go.mod")
	writeRouterFixture(t, root, "web/app.ts")
	writeRouterFixture(t, root, "web/package.json")
	writeRouterFixture(t, root, "node_modules/ignored.py")
	revision := initRouterRepository(t, root)
	goAnalyzer := &fixtureAnalyzer{name: "go", language: "go"}
	tsAnalyzer := &fixtureAnalyzer{name: "ts", language: "typescript"}
	pythonAnalyzer := &fixtureAnalyzer{name: "python", language: "python"}
	router, err := New(
		Candidate{Analyzer: goAnalyzer, Extensions: []string{".go"}, Markers: []string{"go.mod"}},
		Candidate{Analyzer: tsAnalyzer, Extensions: []string{".ts"}, Markers: []string{"package.json"}},
		Candidate{Analyzer: pythonAnalyzer, Extensions: []string{".py"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := router.Analyze(context.Background(), codegraph.Request{
		RepositoryPath: root, RepositoryName: "fixture-repository", Revision: revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if goAnalyzer.calls != 1 || tsAnalyzer.calls != 1 || pythonAnalyzer.calls != 0 {
		t.Fatalf("analyzer calls: go=%d ts=%d python=%d", goAnalyzer.calls, tsAnalyzer.calls, pythonAnalyzer.calls)
	}
	if snapshot.Analyzer != "multi-language" || snapshot.Statistics["analyzers"] != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.RepositoryName != "fixture-repository" || snapshot.Branch == "" {
		t.Fatalf("repository mapping = %#v", snapshot)
	}
}

func TestRouterRequiresProjectMarker(t *testing.T) {
	root := t.TempDir()
	writeRouterFixture(t, root, "example.java")
	initRouterRepository(t, root)
	jvmAnalyzer := &fixtureAnalyzer{name: "jvm", language: "java"}
	router, err := New(Candidate{
		Analyzer: jvmAnalyzer, Extensions: []string{".java"}, Markers: []string{"pom.xml", "build.gradle"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.Analyze(context.Background(), codegraph.Request{RepositoryPath: root}); err == nil {
		t.Fatal("expected unsupported repository error")
	}
	if jvmAnalyzer.calls != 0 {
		t.Fatal("analyzer ran without a project marker")
	}
}

func TestRouterRejectsUnsupportedRepository(t *testing.T) {
	root := t.TempDir()
	writeRouterFixture(t, root, "README.md")
	initRouterRepository(t, root)
	router, err := New(Candidate{Analyzer: &fixtureAnalyzer{name: "go", language: "go"}, Extensions: []string{".go"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.Analyze(context.Background(), codegraph.Request{RepositoryPath: root}); err == nil {
		t.Fatal("expected unsupported repository error")
	}
}

func writeRouterFixture(t *testing.T, root, name string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func initRouterRepository(t *testing.T, root string) string {
	t.Helper()
	commands := [][]string{
		{"git", "init", "--quiet", root},
		{"git", "-C", root, "add", "."},
		{"git", "-C", root, "-c", "user.name=Router Test", "-c", "user.email=router@example.test", "commit", "--quiet", "-m", "fixture"},
	}
	for _, arguments := range commands {
		if output, err := exec.Command(arguments[0], arguments[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", arguments, err, output)
		}
	}
	output, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(output[:len(output)-1])
}
