// Package migrations applies embedded SQL files in filename order.
// Each unapplied migration and its version record share one transaction.
package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version text PRIMARY KEY,
        applied_at timestamptz NOT NULL DEFAULT now()
    )`); err != nil {
		return fmt.Errorf("initialize migrations: %w", err)
	}

	entries, err := fs.Glob(files, "*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	slices.Sort(entries)
	for _, name := range entries {
		var applied bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", name).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied {
			continue
		}
		contents, err := files.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if err := applyMigration(ctx, pool, name, contents); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration owns one transaction so cleanup finishes on each iteration.
func applyMigration(ctx context.Context, pool *pgxpool.Pool, name string, contents []byte) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	// A committed transaction is already closed; on other rollback failures pgx
	// closes the connection. Preserve the original execution or commit error.
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, string(contents)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", strings.TrimSpace(name)); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	return nil
}
