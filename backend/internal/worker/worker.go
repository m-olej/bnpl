package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/molej/bnpl/backend/internal/ledger"
)

// Config tunes a Worker's polling cycle.
type Config struct {
	// BatchSize caps how many installments are claimed per tick.
	BatchSize int32
	// Interval is the delay between poll cycles in Run.
	Interval time.Duration
	// StaleAfter is how long an installment may sit in PROCESSING before it is
	// considered abandoned by a crashed worker and requeued.
	StaleAfter time.Duration
}

// DefaultConfig returns the configuration used by the worker command.
func DefaultConfig() Config {
	return Config{
		BatchSize:  50,
		Interval:   15 * time.Second,
		StaleAfter: 5 * time.Minute,
	}
}

// Stats summarises one poll cycle.
type Stats struct {
	Requeued  int64
	Claimed   int
	Collected int
	Failed    int
}

// Ledger is the subset of the ledger repository the worker depends on. Depending
// on an interface keeps the poll loop unit-testable without a database.
type Ledger interface {
	RequeueStaleProcessing(ctx context.Context, olderThan time.Time) (int64, error)
	ClaimDueInstallments(ctx context.Context, asOf time.Time, limit int32) ([]ledger.Installment, error)
	FailInstallment(ctx context.Context, installmentID int64, reason string) error
	PayInstallment(ctx context.Context, installmentID int64, occurredAt time.Time, traceID string, idempotencyKey *string) (ledger.PayInstallmentResult, error)
	SettleMerchantPayout(ctx context.Context, orderID int64, occurredAt time.Time, traceID string, idempotencyKey *string) (ledger.PayoutResult, error)
}

// Worker polls for due installments and settles them.
type Worker struct {
	repo    Ledger
	gateway Gateway
	logger  *slog.Logger
	cfg     Config
	now     func() time.Time
}

// New builds a Worker. Zero-valued config fields fall back to DefaultConfig.
func New(repo Ledger, gateway Gateway, logger *slog.Logger, cfg Config) *Worker {
	def := DefaultConfig()
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = def.BatchSize
	}
	if cfg.Interval <= 0 {
		cfg.Interval = def.Interval
	}
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = def.StaleAfter
	}
	return &Worker{
		repo:    repo,
		gateway: gateway,
		logger:  logger,
		cfg:     cfg,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// Tick runs a single poll cycle: requeue stale claims, claim due installments,
// capture each through the gateway, and settle the merchant payable for
// successful collections.
func (w *Worker) Tick(ctx context.Context) (Stats, error) {
	var stats Stats
	now := w.now()

	requeued, err := w.repo.RequeueStaleProcessing(ctx, now.Add(-w.cfg.StaleAfter))
	if err != nil {
		return stats, err
	}
	stats.Requeued = requeued
	if requeued > 0 {
		w.logger.Info("requeued stale processing installments", slog.Int64("count", requeued))
	}

	claimed, err := w.repo.ClaimDueInstallments(ctx, now, w.cfg.BatchSize)
	if err != nil {
		return stats, err
	}
	stats.Claimed = len(claimed)

	for _, inst := range claimed {
		traceID := "wrk_" + uuid.NewString()
		log := w.logger.With(
			slog.Int64("installment_id", inst.ID),
			slog.Int64("order_id", inst.OrderID),
			slog.String("trace_id", traceID),
		)

		if err := w.gateway.Capture(ctx, inst); err != nil {
			log.Warn("gateway capture failed", slog.String("error", err.Error()))
			if ferr := w.repo.FailInstallment(ctx, inst.ID, err.Error()); ferr != nil {
				log.Error("recording installment failure", slog.String("error", ferr.Error()))
			}
			stats.Failed++
			continue
		}

		paid, err := w.repo.PayInstallment(ctx, inst.ID, now, traceID, nil)
		if err != nil {
			log.Error("recording installment payment", slog.String("error", err.Error()))
			stats.Failed++
			continue
		}
		stats.Collected++

		payout, err := w.repo.SettleMerchantPayout(ctx, paid.OrderID, now, traceID, nil)
		if err != nil {
			// The collection is durably recorded; settlement is delta-based and
			// will be retried on a later tick.
			log.Error("settling merchant payout", slog.String("error", err.Error()))
			continue
		}
		if payout.PayoutAmount > 0 {
			log.Info("collected installment and settled merchant payout",
				slog.Int64("payout_amount", payout.PayoutAmount),
				slog.String("order_status", paid.OrderStatus))
		}
	}
	return stats, nil
}

// Run polls on the configured interval until ctx is cancelled. Tick errors are
// logged and do not stop the loop; the returned error is ctx.Err().
func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	for {
		if _, err := w.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
			w.logger.Error("worker tick failed", slog.String("error", err.Error()))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// SetClock overrides the worker clock (tests only).
func (w *Worker) SetClock(now func() time.Time) {
	if now != nil {
		w.now = now
	}
}

// String implements fmt.Stringer for logging.
func (w *Worker) String() string {
	return fmt.Sprintf("worker(batch=%d interval=%s stale_after=%s)",
		w.cfg.BatchSize, w.cfg.Interval, w.cfg.StaleAfter)
}
