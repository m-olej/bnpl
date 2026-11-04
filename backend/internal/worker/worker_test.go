package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/molej/bnpl/backend/internal/ledger"
)

type fakeLedger struct {
	requeued   int64
	requeueErr error

	claimed  []ledger.Installment
	claimErr error

	failed  []int64
	failErr error

	paid     []int64
	payErr   error
	payState string

	settled      []int64
	settleErr    error
	payoutAmount int64
}

func (f *fakeLedger) RequeueStaleProcessing(context.Context, time.Time) (int64, error) {
	return f.requeued, f.requeueErr
}

func (f *fakeLedger) ClaimDueInstallments(context.Context, time.Time, int32) ([]ledger.Installment, error) {
	return f.claimed, f.claimErr
}

func (f *fakeLedger) FailInstallment(_ context.Context, id int64, _ string) error {
	f.failed = append(f.failed, id)
	return f.failErr
}

func (f *fakeLedger) PayInstallment(_ context.Context, id int64, _ time.Time, _ string, _ *string) (ledger.PayInstallmentResult, error) {
	if f.payErr != nil {
		return ledger.PayInstallmentResult{}, f.payErr
	}
	f.paid = append(f.paid, id)
	status := f.payState
	if status == "" {
		status = "ACTIVE"
	}
	return ledger.PayInstallmentResult{InstallmentID: id, OrderID: id * 10, OrderStatus: status}, nil
}

func (f *fakeLedger) SettleMerchantPayout(_ context.Context, orderID int64, _ time.Time, _ string, _ *string) (ledger.PayoutResult, error) {
	if f.settleErr != nil {
		return ledger.PayoutResult{}, f.settleErr
	}
	f.settled = append(f.settled, orderID)
	return ledger.PayoutResult{OrderID: orderID, PayoutAmount: f.payoutAmount}, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestTick(t *testing.T) {
	installment := ledger.Installment{ID: 7, OrderID: 70, Amount: 1000}

	tests := []struct {
		name       string
		ledger     *fakeLedger
		gateway    Gateway
		wantStats  Stats
		wantPaid   []int64
		wantFailed []int64
		wantSettle []int64
		wantErr    bool
	}{
		{
			name:       "successful capture pays and settles",
			ledger:     &fakeLedger{requeued: 2, claimed: []ledger.Installment{installment}, payoutAmount: 950},
			gateway:    SimulatedGateway{},
			wantStats:  Stats{Requeued: 2, Claimed: 1, Collected: 1},
			wantPaid:   []int64{7},
			wantSettle: []int64{70},
		},
		{
			name:       "gateway failure marks failed and skips payment",
			ledger:     &fakeLedger{claimed: []ledger.Installment{installment}},
			gateway:    GatewayFunc(func(context.Context, ledger.Installment) error { return errors.New("card declined") }),
			wantStats:  Stats{Claimed: 1, Failed: 1},
			wantFailed: []int64{7},
		},
		{
			name:      "claim error aborts the tick",
			ledger:    &fakeLedger{claimErr: errors.New("boom")},
			gateway:   SimulatedGateway{},
			wantStats: Stats{},
			wantErr:   true,
		},
		{
			name:      "requeue error aborts the tick",
			ledger:    &fakeLedger{requeueErr: errors.New("boom")},
			gateway:   SimulatedGateway{},
			wantStats: Stats{},
			wantErr:   true,
		},
		{
			name:      "nothing due is a clean no-op",
			ledger:    &fakeLedger{},
			gateway:   SimulatedGateway{},
			wantStats: Stats{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := New(tt.ledger, tt.gateway, testLogger(), Config{})
			stats, err := w.Tick(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if stats != tt.wantStats {
				t.Errorf("stats = %+v, want %+v", stats, tt.wantStats)
			}
			assertIDs(t, "paid", tt.ledger.paid, tt.wantPaid)
			assertIDs(t, "failed", tt.ledger.failed, tt.wantFailed)
			assertIDs(t, "settled", tt.ledger.settled, tt.wantSettle)
		})
	}
}

func TestTickPaymentErrorIsCounted(t *testing.T) {
	fl := &fakeLedger{claimed: []ledger.Installment{{ID: 1, OrderID: 10}}, payErr: errors.New("db down")}
	w := New(fl, SimulatedGateway{}, testLogger(), Config{})

	stats, err := w.Tick(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Failed != 1 || stats.Collected != 0 {
		t.Errorf("stats = %+v, want Failed=1 Collected=0", stats)
	}
}

func assertIDs(t *testing.T, name string, got, want []int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}
