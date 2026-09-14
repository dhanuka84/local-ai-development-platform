package execution

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFilePatchAppliesExactContents(t *testing.T) {
	root := t.TempDir()
	base := map[string]string{"old.py": "VALUE = 0\n", "no-newline.txt": "before"}
	next := map[string]string{"old.py": "VALUE = 1\n", "no-newline.txt": "after", "new.py": "def normalize(value):\n    return value.casefold()\n"}
	for name, body := range base {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	patch, err := FilePatch(base, next)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "change.patch")
	if err = os.WriteFile(path, []byte(patch), 0600); err != nil {
		t.Fatal(err)
	}
	init := exec.Command("git", "init", "--quiet", root)
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	cmd := exec.Command("git", "apply", "--", path)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out), patch)
	}
	for name, body := range next {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(got) != body {
			t.Fatal("patch did not preserve exact file", name, err)
		}
	}
	if _, err = FilePatch(nil, map[string]string{"../escape": "outside"}); err == nil {
		t.Fatal("path escape accepted")
	}
}
