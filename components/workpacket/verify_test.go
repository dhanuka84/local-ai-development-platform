package workpacket

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyPatchUsesDisposableCloneAndEnforcesScope(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	repository := t.TempDir()
	runGitTest(t, repository, "init", "--quiet")
	runGitTest(t, repository, "config", "user.name", "Work Packet Test")
	runGitTest(t, repository, "config", "user.email", "workpacket@example.test")
	file := filepath.Join(repository, "app.txt")
	if err := os.WriteFile(file, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repository, "add", "app.txt")
	runGitTest(t, repository, "commit", "--quiet", "-m", "base")
	base := strings.TrimSpace(runGitTest(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(file, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := []byte(runGitTest(t, repository, "diff", "--", "app.txt"))
	runGitTest(t, repository, "checkout", "--", "app.txt")

	packet := validPatchPacket()
	packet.Workspace = repository
	packet.BaseRevision = base
	packet.AllowedFiles = []string{"app.txt"}
	packet.Checks = []Check{{Name: "diff-check", Argv: []string{"git", "diff", "--cached", "--check"}, TimeoutSeconds: 10}}
	packet.Limits = Limits{MaxChangedFiles: 1, MaxDiffLines: 5, MaxPatchBytes: 10000}
	result := VerifyPatch(context.Background(), packet, patch)
	if !result.Accepted || len(result.ChangedFiles) != 1 || result.ChangedFiles[0].Path != "app.txt" {
		t.Fatalf("verification failed: %#v", result)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "before\n" {
		t.Fatalf("source checkout changed: %q", content)
	}

	packet.AllowedFiles = []string{"docs/**"}
	result = VerifyPatch(context.Background(), packet, patch)
	if result.Accepted || !containsText(result.Errors, "outside the work-packet scope") {
		t.Fatalf("out-of-scope patch accepted: %#v", result)
	}
}

func TestVerifyPatchRejectsCheckSideEffects(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	repository := t.TempDir()
	runGitTest(t, repository, "init", "--quiet")
	runGitTest(t, repository, "config", "user.name", "Work Packet Test")
	runGitTest(t, repository, "config", "user.email", "workpacket@example.test")
	file := filepath.Join(repository, "app.txt")
	if err := os.WriteFile(file, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repository, "add", "app.txt")
	runGitTest(t, repository, "commit", "--quiet", "-m", "base")
	base := strings.TrimSpace(runGitTest(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(file, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := []byte(runGitTest(t, repository, "diff", "--", "app.txt"))
	runGitTest(t, repository, "checkout", "--", "app.txt")

	packet := validPatchPacket()
	packet.Workspace = repository
	packet.BaseRevision = base
	packet.AllowedFiles = []string{"app.txt"}
	packet.Checks = []Check{{Name: "side-effect", Argv: []string{"git", "checkout", "HEAD", "--", "app.txt"}, TimeoutSeconds: 10}}
	packet.Limits = Limits{MaxChangedFiles: 1, MaxDiffLines: 5}
	result := VerifyPatch(context.Background(), packet, patch)
	if result.Accepted || !containsText(result.Errors, "changed the staged patch") {
		t.Fatalf("check side effect was not rejected: %#v", result)
	}
}

func runGitTest(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func containsText(values []string, expected string) bool {
	for _, value := range values {
		if strings.Contains(value, expected) {
			return true
		}
	}
	return false
}
