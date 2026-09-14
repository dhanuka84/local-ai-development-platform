package sdlcworker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
)

func TestSnapshotExcludesHistoryConfigAndDirtyFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, source, "init", "--quiet"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "module.py"), []byte("VALUE = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, source, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, source, "-c", "user.name=Synthetic", "-c", "user.email=test@example.invalid", "commit", "--quiet", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	base, err := git(ctx, source, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = git(ctx, source, "config", "credential.helper", "synthetic-sensitive-helper"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, ".env"), []byte("SYNTHETIC_SECRET=must-not-copy\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "module.py"), []byte("VALUE = 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "snapshot")
	if err = snapshot(ctx, source, strings.TrimSpace(string(base)), destination); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(destination, ".env")); !os.IsNotExist(err) {
		t.Fatal("untracked credential file entered sandbox snapshot")
	}
	content, err := os.ReadFile(filepath.Join(destination, "module.py"))
	if err != nil || string(content) != "VALUE = 1\n" {
		t.Fatal("snapshot includes dirty files")
	}
	config, err := os.ReadFile(filepath.Join(destination, ".git", "config"))
	if err != nil || strings.Contains(string(config), "credential") || strings.Contains(string(config), "remote") {
		t.Fatal("source credential configuration entered sandbox snapshot")
	}
	packet := workpacket.Packet{SchemaVersion: workpacket.SchemaVersion, ID: "fixture", Goal: "Update module", BaseRevision: strings.TrimSpace(string(base)), Mode: workpacket.ModePatch, TaskClass: workpacket.TaskDevelopment, DataClassification: workpacket.DataInternal, LocalOnly: true, AllowedFiles: []string{"module.py", ".env"}, ForbiddenFiles: []string{"protected_tests"}, Rollback: []string{"discard"}, Checks: []workpacket.Check{{Name: "check", Argv: []string{"true"}}}, Limits: workpacket.Limits{MaxChangedFiles: 1, MaxDiffLines: 10}}
	files, err := builderFiles(ctx, source, packet)
	if err != nil || files["module.py"] != "VALUE = 1\n" {
		t.Fatal("builder did not read the exact accepted source revision", err)
	}
}
