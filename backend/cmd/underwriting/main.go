// Command underwriting runs the Underwriting HTTP service.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/molej/bnpl/backend/internal/idempotency"
	"github.com/molej/bnpl/backend/internal/ledger"
	"github.com/molej/bnpl/backend/internal/platform/database"
	"github.com/molej/bnpl/backend/internal/platform/logging"
	"github.com/molej/bnpl/backend/internal/platform/server"
	"github.com/molej/bnpl/backend/internal/underwriting"
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

	db, err := database.Open(ctx, database.DefaultConfig(dsn))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	repo := ledger.NewRepository(db)
	svc := underwriting.NewService(repo)
	store := idempotency.NewMySQLStore(db)

	srv := &http.Server{
		Addr:              server.Env("UNDERWRITING_HTTP_ADDR", ":8082"),
		Handler:           underwriting.NewRouter(svc, store, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return server.Run(ctx, logger, srv)
}
