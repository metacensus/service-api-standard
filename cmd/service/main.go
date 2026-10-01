// Command service is the MetaCensus API over Postgres: it wires a
// postgres-backed store.Store into metacensus/api's go/service and serves it.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metacensus/api/go/service"

	"github.com/metacensus/service-api-standard/internal/config"
	"github.com/metacensus/service-api-standard/internal/postgres"
	"github.com/metacensus/service-api-standard/internal/server"
)

const bootTimeout = 30 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	os.Exit(run(logger))
}

// run returns the exit code, so deferred cleanup (the pool, the signal
// handler) runs before the process exits.
func run(logger *slog.Logger) int {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		logger.Error("refusing to start", slog.String("error", err.Error()))
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	pool, err := openDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("refusing to start", slog.String("error", err.Error()))
		return 1
	}
	defer pool.Close()

	handlers := service.New(service.Config{Store: postgres.New(pool)})
	if err := server.New(cfg.Port, handlers, logger).Run(ctx); err != nil {
		logger.Error("server stopped", slog.String("error", err.Error()))
		return 1
	}
	logger.Info("stopped cleanly", slog.String("event", "stopped"))
	return 0
}

func openDatabase(ctx context.Context, url string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, bootTimeout)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err == nil {
		err = postgres.Migrate(ctx, pool)
	}
	if err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
