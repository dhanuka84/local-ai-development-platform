// SPDX-License-Identifier: MPL-2.0

package codegraph

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRepositoryStateMapsAndValidatesBranch(t *testing.T) {
	root := t.TempDir()
	commands := [][]string{
		{"git", "init", "--quiet", "--initial-branch=main", root},
		{"git", "-C", root, "add", "."},
		{"git", "-C", root, "-c", "user.name=Code Graph Test", "-c", "user.email=codegraph@example.test", "commit", "--quiet", "--allow-empty", "-m", "fixture"},
	}
	for _, arguments := range commands {
		if output, err := exec.Command(arguments[0], arguments[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", arguments, err, output)
		}
	}
	revisionOutput, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	revision := strings.TrimSpace(string(revisionOutput))

	gotRevision, gotBranch, dirty, err := ResolveRepositoryState(context.Background(), root, revision, "main")
	if err != nil {
		t.Fatal(err)
	}
	if gotRevision != revision || gotBranch != "main" || dirty {
		t.Fatalf("state = revision %q branch %q dirty %t", gotRevision, gotBranch, dirty)
	}
	if _, _, _, err := ResolveRepositoryState(context.Background(), root, revision, "feature"); err == nil {
		t.Fatal("expected branch mismatch")
	}

	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, dirty, err = ResolveRepositoryState(context.Background(), root, revision, "main")
	if err != nil || !dirty {
		t.Fatalf("dirty state = %t, %v", dirty, err)
	}
}
