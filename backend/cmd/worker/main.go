// Command worker runs the background installment-settlement loop (docs/0003).
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/molej/bnpl/backend/internal/ledger"
	"github.com/molej/bnpl/backend/internal/platform/database"
	"github.com/molej/bnpl/backend/internal/platform/logging"
	"github.com/molej/bnpl/backend/internal/platform/server"
	"github.com/molej/bnpl/backend/internal/worker"
)

func main() {
	if err := run(); err != nil {
		logging.New("error").Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	logger := logging.New(server.Env("LOG_LEVEL", "info"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dsn, err := server.MustEnv("DB_DSN")
	if err != nil {
		return err
	}

	cfg, err := workerConfig()
	if err != nil {
		return err
	}

	db, err := database.Open(ctx, database.DefaultConfig(dsn))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	repo := ledger.NewRepository(db)
	wk := worker.New(repo, worker.SimulatedGateway{}, logger, cfg)
	logger.Info("worker starting", slog.String("config", wk.String()))

	if err := wk.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("worker stopped")
	return nil
}

func workerConfig() (worker.Config, error) {
	cfg := worker.DefaultConfig()

	if v := server.Env("WORKER_BATCH_SIZE", ""); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n <= 0 {
			return cfg, errors.New("WORKER_BATCH_SIZE must be a positive integer")
		}
		cfg.BatchSize = int32(n)
	}
	if v := server.Env("WORKER_POLL_INTERVAL", ""); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return cfg, errors.New("WORKER_POLL_INTERVAL must be a positive duration (e.g. 15s)")
		}
		cfg.Interval = d
	}
	if v := server.Env("WORKER_STALE_AFTER", ""); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return cfg, errors.New("WORKER_STALE_AFTER must be a positive duration (e.g. 5m)")
		}
		cfg.StaleAfter = d
	}
	return cfg, nil
}
