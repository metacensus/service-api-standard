//go:build integration

// Package pgtest gives integration tests a migrated Postgres. One container
// starts per test binary, with the schema applied once into a template
// database; Shared and Fresh hand out databases cloned from it. Nothing stops
// the container: testcontainers' Ryuk reaper removes it when the test process
// exits.
package pgtest

import (
	"context"
	"crypto/rand"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metacensus/service-api-standard/internal/postgres"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	image    = "postgres:18-alpine"
	template = "template_migrated"
)

var (
	once      sync.Once
	container *tcpostgres.PostgresContainer
	baseURL   string
	startErr  error

	sharedOnce sync.Once
	sharedDB   string
	sharedErr  error

	logsOnce sync.Once
)

// Shared returns a pool on the one migrated database the test binary shares.
// Use it for tests that mint their own ids and read only what they wrote.
func Shared(t *testing.T) *pgxpool.Pool {
	t.Helper()
	start(t)
	sharedOnce.Do(func() { sharedDB, sharedErr = clone(t.Context()) })
	if sharedErr != nil {
		t.Fatalf("pgtest: shared database: %v", sharedErr)
	}
	return connect(t, sharedDB)
}

// Fresh returns a pool on a new database cloned from the migrated template, for
// tests that assert state nobody else may touch: empty lists, row counts,
// migrations.
func Fresh(t *testing.T) *pgxpool.Pool {
	t.Helper()
	start(t)
	db, err := clone(t.Context())
	if err != nil {
		t.Fatalf("pgtest: clone template: %v", err)
	}
	return connect(t, db)
}

func start(t *testing.T) {
	t.Helper()
	once.Do(func() { startErr = boot() })
	if startErr != nil {
		t.Fatalf("pgtest: %v", startErr)
	}
	t.Cleanup(func() {
		if t.Failed() {
			logsOnce.Do(func() { printLogs(t) })
		}
	})
}

func boot() error {
	ctx := context.Background()
	var err error
	container, err = tcpostgres.Run(ctx, image,
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return err
	}
	if baseURL, err = container.ConnectionString(ctx); err != nil {
		return err
	}
	if err := admin(ctx, "CREATE DATABASE "+template); err != nil {
		return err
	}
	pool, err := open(ctx, template)
	if err != nil {
		return err
	}
	// Closed before any clone: CREATE DATABASE ... TEMPLATE fails while the
	// template has a connection.
	defer pool.Close()
	return postgres.Migrate(ctx, pool)
}

func admin(ctx context.Context, stmt string) error {
	conn, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, stmt)
	return err
}

func open(ctx context.Context, db string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(baseURL)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.Database = db
	return pgxpool.NewWithConfig(ctx, cfg)
}

func clone(ctx context.Context) (string, error) {
	db := "test_" + strings.ToLower(rand.Text())
	return db, admin(ctx, "CREATE DATABASE "+db+" TEMPLATE "+template)
}

func connect(t *testing.T, db string) *pgxpool.Pool {
	t.Helper()
	pool, err := open(t.Context(), db)
	if err != nil {
		t.Fatalf("pgtest: connect %s: %v", db, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// printLogs writes the container's recent output.
func printLogs(t *testing.T) {
	r, err := container.Logs(context.Background())
	if err != nil {
		return
	}
	defer r.Close()
	b, _ := io.ReadAll(r)
	if len(b) > 4096 {
		b = b[len(b)-4096:]
	}
	t.Logf("postgres container logs (tail):\n%s", b)
}
