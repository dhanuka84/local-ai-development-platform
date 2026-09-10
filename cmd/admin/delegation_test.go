package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
)

func TestCredentialOutputRefusesUnsafeOrExistingFile(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "token")
	if err := os.WriteFile(file, []byte("existing credential"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := taskCredential(context.Background(), config.Config{}, []string{"delegate-task", "task", "60", file}); err == nil {
		t.Fatal("existing file overwritten")
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "existing credential" {
		t.Fatal("existing credential changed")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if err := taskCredential(context.Background(), config.Config{}, []string{"delegate-task", "task", "60", link}); err == nil {
		t.Fatal("symlink output accepted")
	}
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := taskCredential(context.Background(), config.Config{}, []string{"delegate-task", "task", "60", filepath.Join(root, "new")}); err == nil {
		t.Fatal("public parent directory accepted")
	}
}
