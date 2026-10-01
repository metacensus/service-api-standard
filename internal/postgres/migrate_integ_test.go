//go:build integration

package postgres_test

import (
	"context"
	"sync"
	"testing"

	"github.com/metacensus/service-api-standard/internal/postgres"
	"github.com/metacensus/service-api-standard/internal/postgres/pgtest"
)

func TestMigrate_Idempotent(t *testing.T) {
	pool := pgtest.Fresh(t)
	ctx := context.Background()

	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("re-running Migrate: %v", err)
	}
	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before == 0 || after != before {
		t.Errorf("schema_migrations rows = %d then %d, want equal and non-zero", before, after)
	}
}

func TestMigrate_Concurrent(t *testing.T) {
	pool := pgtest.Fresh(t)
	if _, err := pool.Exec(context.Background(), `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}

	const replicas = 8
	errs := make(chan error, replicas)
	var wg sync.WaitGroup
	for range replicas {
		wg.Go(func() { errs <- postgres.Migrate(context.Background(), pool) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Migrate: %v", err)
		}
	}
}
