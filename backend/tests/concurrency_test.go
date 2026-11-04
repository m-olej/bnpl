//go:build integration

package tests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/molej/bnpl/backend/internal/ledger"
	"github.com/molej/bnpl/backend/internal/platform/database"
	"github.com/molej/bnpl/backend/internal/platform/logging"
	"github.com/molej/bnpl/backend/internal/worker"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(context.Background(), database.DefaultConfig(testDSN(t)))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustCustomerAndMerchant(t *testing.T, repo *ledger.Repository, suffix int64, creditLimit int64, feeBps int) (ledger.Customer, ledger.Merchant) {
	t.Helper()
	ctx := context.Background()
	customer, err := repo.CreateCustomer(ctx, fmt.Sprintf("ccust-%d-%d", suffix, feeBps), "Concurrency Customer", creditLimit, "USD")
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	merchant, err := repo.CreateMerchant(ctx, fmt.Sprintf("cmerch-%d-%d", suffix, feeBps), "Concurrency Merchant", feeBps, "USD")
	if err != nil {
		t.Fatalf("create merchant: %v", err)
	}
	return customer, merchant
}

// TestConcurrentAuthorizationsRespectCreditLimit proves the customer-row lock in
// CreateOrderWithSchedule serializes concurrent authorizations: with a 10000
// limit and 6000 orders, exactly one of six contenders may succeed.
func TestConcurrentAuthorizationsRespectCreditLimit(t *testing.T) {
	ctx := context.Background()
	repo := ledger.NewRepository(openDB(t))
	suffix := time.Now().UnixNano()
	customer, merchant := mustCustomerAndMerchant(t, repo, suffix, 10000, 0)

	const contenders = 6
	errs := make([]error, contenders)
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := repo.CreateOrderWithSchedule(ctx, ledger.CreateOrderParams{
				OrderRef:         fmt.Sprintf("conc-%d-%d", suffix, i),
				CustomerID:       customer.ID,
				MerchantID:       merchant.ID,
				PrincipalAmount:  6000,
				Currency:         "USD",
				InstallmentCount: 1,
				FirstDueDate:     time.Now().UTC(),
				TraceID:          "concurrency",
			})
			errs[i] = err
		}(i)
	}
	wg.Wait()

	successes := 0
	for i, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ledger.ErrInsufficientCredit):
			// expected for the losers
		default:
			t.Errorf("contender %d unexpected error: %v", i, err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful authorizations = %d, want 1", successes)
	}

	available, err := repo.AvailableCredit(ctx, customer.ID)
	if err != nil {
		t.Fatalf("available credit: %v", err)
	}
	if available != 4000 {
		t.Errorf("available credit = %d, want 4000", available)
	}
}

// TestClaimDueInstallmentsSkipsLocked proves SELECT ... FOR UPDATE SKIP LOCKED
// skips rows already locked by another session instead of blocking on them: a
// claim must return every other due installment but not the locked one.
func TestClaimDueInstallmentsSkipsLocked(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	repo := ledger.NewRepository(db)
	suffix := time.Now().UnixNano()
	customer, merchant := mustCustomerAndMerchant(t, repo, suffix, 1000000, 0)

	const total = 20
	result, err := repo.CreateOrderWithSchedule(ctx, ledger.CreateOrderParams{
		OrderRef:         fmt.Sprintf("claim-%d", suffix),
		CustomerID:       customer.ID,
		MerchantID:       merchant.ID,
		PrincipalAmount:  total * 100,
		Currency:         "USD",
		InstallmentCount: total,
		FirstDueDate:     time.Now().UTC().Add(-2 * time.Hour),
		TraceID:          "concurrency",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	target := make(map[int64]bool, total)
	for _, inst := range result.Installments {
		target[inst.ID] = true
	}
	lockedID := result.Installments[0].ID

	// Hold a pessimistic lock on one target installment from a second session.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer func() { _ = conn.Close() }()

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	var held int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM installments WHERE id = ? FOR UPDATE", lockedID).Scan(&held); err != nil {
		t.Fatalf("lock installment %d: %v", lockedID, err)
	}

	// A claim with a generous limit must skip the locked row and take the rest.
	claimed, err := repo.ClaimDueInstallments(ctx, time.Now().UTC(), 100)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	seen := make(map[int64]bool, len(claimed))
	for _, inst := range claimed {
		if seen[inst.ID] {
			t.Errorf("installment %d claimed twice in one batch", inst.ID)
		}
		seen[inst.ID] = true
	}
	if seen[lockedID] {
		t.Errorf("claimed installment %d held by another session; SKIP LOCKED failed", lockedID)
	}
	for id := range target {
		if id == lockedID {
			continue
		}
		if !seen[id] {
			t.Errorf("due target installment %d was not claimed", id)
		}
	}
}

// TestConcurrentRefundsRespectOutstanding proves the order-row lock serializes
// refunds: two full-principal refunds cannot both apply.
func TestConcurrentRefundsRespectOutstanding(t *testing.T) {
	ctx := context.Background()
	repo := ledger.NewRepository(openDB(t))
	suffix := time.Now().UnixNano()
	customer, merchant := mustCustomerAndMerchant(t, repo, suffix, 1000000, 250)
	orderRef := fmt.Sprintf("refund-race-%d", suffix)

	if _, err := repo.CreateOrderWithSchedule(ctx, ledger.CreateOrderParams{
		OrderRef:         orderRef,
		CustomerID:       customer.ID,
		MerchantID:       merchant.ID,
		PrincipalAmount:  10000,
		Currency:         "USD",
		InstallmentCount: 2,
		FirstDueDate:     time.Now().UTC(),
		TraceID:          "concurrency",
	}); err != nil {
		t.Fatalf("create order: %v", err)
	}

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := repo.RefundOrder(ctx, ledger.RefundParams{OrderRef: orderRef, Amount: 10000})
			errs[i] = err
		}(i)
	}
	wg.Wait()

	successes := 0
	for i, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ledger.ErrRefundExceedsOutstanding):
		default:
			t.Errorf("refund %d unexpected error: %v", i, err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful refunds = %d, want 1", successes)
	}

	order, err := repo.GetOrderByRef(ctx, orderRef)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if order.Status != "REFUNDED" {
		t.Errorf("order status = %s, want REFUNDED", order.Status)
	}
	receivable := mustAccount(t, ctx, repo, ledger.CodeCustomerReceivable, ledger.OwnerCustomer, customer.ID)
	if bal := mustBalance(t, ctx, repo, receivable.ID); bal.Net() != 0 {
		t.Errorf("receivable net = %d, want 0", bal.Net())
	}
}

// TestConcurrentPayoutSettlementIsIdempotent proves the delta-based settlement
// cannot double-pay when several workers settle the same fully-collected order.
func TestConcurrentPayoutSettlementIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := ledger.NewRepository(openDB(t))
	suffix := time.Now().UnixNano()
	customer, merchant := mustCustomerAndMerchant(t, repo, suffix, 1000000, 250)
	orderRef := fmt.Sprintf("payout-race-%d", suffix)

	result, err := repo.CreateOrderWithSchedule(ctx, ledger.CreateOrderParams{
		OrderRef:         orderRef,
		CustomerID:       customer.ID,
		MerchantID:       merchant.ID,
		PrincipalAmount:  10000,
		Currency:         "USD",
		InstallmentCount: 4,
		FirstDueDate:     time.Now().UTC().Add(-time.Hour),
		TraceID:          "concurrency",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	for _, inst := range result.Installments {
		if _, err := repo.PayInstallment(ctx, inst.ID, time.Now().UTC(), "concurrency", nil); err != nil {
			t.Fatalf("pay installment %d: %v", inst.ID, err)
		}
	}

	settlements := make([]ledger.PayoutResult, 2)
	var wg sync.WaitGroup
	for i := range settlements {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := repo.SettleMerchantPayout(ctx, result.OrderID, time.Now().UTC(), "concurrency", nil)
			if err != nil {
				t.Errorf("settle %d: %v", i, err)
				return
			}
			settlements[i] = res
		}(i)
	}
	wg.Wait()

	var total int64
	for _, s := range settlements {
		total += s.PayoutAmount
	}
	if total != 9750 {
		t.Fatalf("total payout = %d, want 9750 (principal 10000 - fee 250)", total)
	}

	payable := mustAccount(t, ctx, repo, ledger.CodeMerchantPayable, ledger.OwnerMerchant, merchant.ID)
	if bal := mustBalance(t, ctx, repo, payable.ID); bal.Signed(ledger.NormalCredit) != 0 {
		t.Errorf("merchant payable = %d, want 0", bal.Signed(ledger.NormalCredit))
	}
}

// TestWorkerTickCollectsAndSettles exercises the full worker path end-to-end
// against MySQL: claim due installments -> gateway capture -> INSTALLMENT_PAID ->
// MERCHANT_PAYOUT, ending with a COMPLETED order and zeroed receivable/payable.
func TestWorkerTickCollectsAndSettles(t *testing.T) {
	ctx := context.Background()
	repo := ledger.NewRepository(openDB(t))
	suffix := time.Now().UnixNano()
	customer, merchant := mustCustomerAndMerchant(t, repo, suffix, 1000000, 250)

	if _, err := repo.CreateOrderWithSchedule(ctx, ledger.CreateOrderParams{
		OrderRef:         fmt.Sprintf("worker-%d", suffix),
		CustomerID:       customer.ID,
		MerchantID:       merchant.ID,
		PrincipalAmount:  10000,
		Currency:         "USD",
		InstallmentCount: 3,
		FirstDueDate:     time.Now().UTC().Add(-2 * time.Hour),
		TraceID:          "worker-e2e",
	}); err != nil {
		t.Fatalf("create order: %v", err)
	}

	wk := worker.New(repo, worker.SimulatedGateway{}, logging.New("error"), worker.Config{
		BatchSize:  100,
		StaleAfter: 5 * time.Minute,
	})
	stats, err := wk.Tick(ctx)
	if err != nil {
		t.Fatalf("worker tick: %v", err)
	}
	if stats.Collected < 3 || stats.Failed != 0 {
		t.Fatalf("stats = %+v, want at least 3 collected and 0 failed", stats)
	}

	order, err := repo.GetOrderByRef(ctx, fmt.Sprintf("worker-%d", suffix))
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if order.Status != "COMPLETED" {
		t.Errorf("order status = %s, want COMPLETED", order.Status)
	}

	receivable := mustAccount(t, ctx, repo, ledger.CodeCustomerReceivable, ledger.OwnerCustomer, customer.ID)
	if bal := mustBalance(t, ctx, repo, receivable.ID); bal.Net() != 0 {
		t.Errorf("receivable net = %d, want 0", bal.Net())
	}
	payable := mustAccount(t, ctx, repo, ledger.CodeMerchantPayable, ledger.OwnerMerchant, merchant.ID)
	if bal := mustBalance(t, ctx, repo, payable.ID); bal.Signed(ledger.NormalCredit) != 0 {
		t.Errorf("merchant payable = %d, want 0", bal.Signed(ledger.NormalCredit))
	}
}
