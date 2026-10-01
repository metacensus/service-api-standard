package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// lockKey is the advisory lock replicas serialize on; arbitrary but fixed.
const lockKey = 0x6d65746163656e73 // "metacens"

// Migrate applies every embedded migration not yet recorded, in filename
// order, each in its own transaction, under an advisory lock so concurrent
// replicas take turns.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	pooled, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	// Hijacked, the connection is closed rather than returned, which drops its
	// session lock on every path, including a failed unlock.
	conn := pooled.Hijack()
	defer conn.Close(context.WithoutCancel(ctx))

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(lockKey)); err != nil {
		return fmt.Errorf("migrate: lock: %w", err)
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    int         PRIMARY KEY,
		name       text        NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	files, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	for _, path := range files { // Glob sorts by name
		name := strings.TrimPrefix(path, "migrations/")
		version, _, _ := strings.Cut(name, "_")
		v, err := strconv.Atoi(version)
		if err != nil {
			return fmt.Errorf("migrate: %s: name must start with a version number", name)
		}
		if err := apply(ctx, conn, path, name, v); err != nil {
			return fmt.Errorf("migrate: %s: %w", name, err)
		}
	}
	return nil
}

func apply(ctx context.Context, conn *pgx.Conn, path, name string, version int) error {
	var done bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&done); err != nil || done {
		return err
	}
	sql, err := migrationFiles.ReadFile(path)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, version, name)
		return err
	})
}
