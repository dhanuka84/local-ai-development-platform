//go:build sdlc_host

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func restoreExecutionFixture(t *testing.T, f *runtime) {
	t.Helper()
	container := os.Getenv("TEST_POSTGRES_CONTAINER")
	if container == "" {
		t.Fatal("missing owned disposable PostgreSQL container")
	}
	f.stopGateway()
	f.stopWorker()
	parsed, err := url.Parse(f.env["DATABASE_URL"])
	if err != nil {
		t.Fatal(err)
	}
	original := parsed.Path[1:]
	restored := original + "_restored"
	dump := exec.CommandContext(f.ctx, "docker", "exec", container, "pg_dump", "-U", "hybrid", "-Fc", original)
	raw, err := dump.Output()
	if err != nil {
		t.Fatal("disposable pg_dump", err)
	}
	report := filepath.Join(os.Getenv("AGENT_READY_REPORT_DIR"), t.Name())
	if err = os.MkdirAll(report, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(report, "database.backup"), raw, 0400); err != nil {
		t.Fatal(err)
	}
	if _, err = f.repo.Pool().Exec(f.ctx, "CREATE DATABASE "+restored+" TEMPLATE template0"); err != nil {
		t.Fatal(err)
	}
	oldRepo := f.repo
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := oldRepo.Pool().Exec(ctx, "DROP DATABASE "+restored+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	restore := exec.CommandContext(f.ctx, "docker", "exec", "-i", container, "pg_restore", "-U", "hybrid", "--no-owner", "-d", restored)
	restore.Stdin = bytes.NewReader(raw)
	if output, err := restore.CombinedOutput(); err != nil {
		t.Fatalf("disposable pg_restore: %v %s", err, output)
	}
	artifactsBefore := f.env["ARTIFACTS_PATH"]
	artifactsAfter := filepath.Join(f.root, "restored-artifacts")
	count := 0
	err = filepath.WalkDir(artifactsBefore, func(path string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		relative, e := filepath.Rel(artifactsBefore, path)
		if e != nil {
			return e
		}
		target := filepath.Join(artifactsAfter, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected artifact type")
		}
		content, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if e = os.WriteFile(target, content, 0400); e != nil {
			return e
		}
		readback, e := os.ReadFile(target)
		if e != nil || domain.Digest(readback) != domain.Digest(content) {
			return domain.ErrEvidenceUnavailable
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + restored
	f.env["DATABASE_URL"] = parsed.String()
	f.env["ARTIFACTS_PATH"] = artifactsAfter
	f.repo, err = postgres.Open(f.ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.repo.Close)
	f.stopGateway = f.start("gateway")
	waitExecutionGateway(f)
	f.stopWorker = f.start("worker")
	evidence := fmt.Sprintf("PostgreSQL native dump SHA256=%s\nRestored content-addressed artifacts=%d\nSource and restored databases are distinct disposable databases.\n", domain.Digest(raw), count)
	if err = os.WriteFile(filepath.Join(report, "restore.txt"), []byte(evidence), 0400); err != nil {
		t.Fatal(err)
	}
}
