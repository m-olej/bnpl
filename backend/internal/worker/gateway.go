// Package worker implements the background settlement loop from docs/0003:
// it claims due installments with SELECT ... FOR UPDATE SKIP LOCKED, captures
// them through a payment gateway, records the collection, and settles the
// merchant's payable for the collected portion.
package worker

import (
	"context"

	"github.com/molej/bnpl/backend/internal/ledger"
)

// Gateway captures a single installment from a customer's funding source.
// Implementations must be safe for concurrent use and honour context
// cancellation.
type Gateway interface {
	Capture(ctx context.Context, installment ledger.Installment) error
}

// SimulatedGateway always succeeds. It stands in for a real payment-gateway
// integration until one is wired up; the worker's locking, persistence and
// settlement logic are exercised regardless.
type SimulatedGateway struct{}

// Capture implements Gateway.
func (SimulatedGateway) Capture(context.Context, ledger.Installment) error {
	return nil
}

// GatewayFunc adapts a function to the Gateway interface (useful in tests).
type GatewayFunc func(ctx context.Context, installment ledger.Installment) error

// Capture implements Gateway.
func (f GatewayFunc) Capture(ctx context.Context, installment ledger.Installment) error {
	return f(ctx, installment)
}
