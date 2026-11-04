//go:build integration

// Package tests contains integration tests that require a real MySQL 8
// instance (docker compose or testcontainers). Run with:
//
//	BNPL_TEST_DSN='bnpl:bnplpw@tcp(127.0.0.1:3306)/bnpl?parseTime=true&loc=UTC' \
//	  go test -race -tags=integration ./tests/...
package tests

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/molej/bnpl/backend/internal/ledger"
	"github.com/molej/bnpl/backend/internal/platform/database"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("BNPL_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("DB_DSN")
	}
	if dsn == "" {
		t.Skip("skipping integration test: set BNPL_TEST_DSN (or DB_DSN) to a migrated MySQL DSN")
	}
	return dsn
}

func TestLedgerLifecycle(t *testing.T) {
	ctx := context.Background()

	db, err := database.Open(ctx, database.DefaultConfig(testDSN(t)))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := ledger.NewRepository(db)
	suffix := time.Now().UnixNano()

	customer, err := repo.CreateCustomer(ctx, fmt.Sprintf("cust-%d", suffix), "Integration Test", 500000, "USD")
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	merchant, err := repo.CreateMerchant(ctx, fmt.Sprintf("merch-%d", suffix), "Integration Merchant", 250, "USD")
	if err != nil {
		t.Fatalf("create merchant: %v", err)
	}

	feeBefore := mustSystemBalance(t, ctx, repo, ledger.CodeMerchantFeeRevenue)

	orderRef := fmt.Sprintf("order-%d", suffix)
	result, err := repo.CreateOrderWithSchedule(ctx, ledger.CreateOrderParams{
		OrderRef:         orderRef,
		CustomerID:       customer.ID,
		MerchantID:       merchant.ID,
		PrincipalAmount:  10000,
		Currency:         "USD",
		InstallmentCount: 3,
		FirstDueDate:     time.Now().UTC().Truncate(time.Second),
		Interval:         14 * 24 * time.Hour,
		TraceID:          "test-trace",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	if result.FeeAmount != 250 {
		t.Errorf("fee = %d, want 250", result.FeeAmount)
	}
	if result.MerchantPayout != 9750 {
		t.Errorf("merchant payout = %d, want 9750", result.MerchantPayout)
	}

	wantAmounts := []int64{3334, 3333, 3333}
	if len(result.Installments) != len(wantAmounts) {
		t.Fatalf("got %d installments, want %d", len(result.Installments), len(wantAmounts))
	}
	for i, inst := range result.Installments {
		if inst.Amount != wantAmounts[i] {
			t.Errorf("installment[%d].Amount = %d, want %d", i, inst.Amount, wantAmounts[i])
		}
		if inst.Number != i+1 {
			t.Errorf("installment[%d].Number = %d, want %d", i, inst.Number, i+1)
		}
	}

	receivable := mustAccount(t, ctx, repo, ledger.CodeCustomerReceivable, ledger.OwnerCustomer, customer.ID)
	if bal := mustBalance(t, ctx, repo, receivable.ID); bal.DebitTotal != 10000 || bal.CreditTotal != 0 {
		t.Errorf("receivable = %+v, want debit 10000 credit 0", bal)
	}

	feeAfter := mustSystemBalance(t, ctx, repo, ledger.CodeMerchantFeeRevenue)
	if delta := feeAfter.Signed(ledger.NormalCredit) - feeBefore.Signed(ledger.NormalCredit); delta != 250 {
		t.Errorf("fee revenue delta = %d, want 250", delta)
	}

	// Collect the whole schedule.
	for i, inst := range result.Installments {
		res, err := repo.PayInstallment(ctx, inst.ID, time.Now().UTC(), "test-trace", nil)
		if err != nil {
			t.Fatalf("pay installment %d: %v", i+1, err)
		}
		wantStatus := "ACTIVE"
		if i == len(result.Installments)-1 {
			wantStatus = "COMPLETED"
		}
		if res.OrderStatus != wantStatus {
			t.Errorf("after installment %d order status = %s, want %s", i+1, res.OrderStatus, wantStatus)
		}
	}

	final := mustBalance(t, ctx, repo, receivable.ID)
	if final.Net() != 0 {
		t.Errorf("receivable net after full repayment = %d, want 0", final.Net())
	}

	order, err := repo.GetOrderByRef(ctx, orderRef)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if order.Status != "COMPLETED" {
		t.Errorf("order status = %s, want COMPLETED", order.Status)
	}
}

func TestLedgerImmutabilityTriggers(t *testing.T) {
	ctx := context.Background()

	db, err := database.Open(ctx, database.DefaultConfig(testDSN(t)))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := ledger.NewRepository(db)
	entryID, err := repo.PostJournal(ctx, ledger.JournalEntryInput{
		EntryType:     ledger.EntryOrderAuthorized,
		ReferenceType: ledger.ReferenceTypeOrder,
		ReferenceID:   999,
		OccurredAt:    time.Now().UTC(),
		Postings: []ledger.PostingInput{
			{AccountID: mustSystemAccountID(t, ctx, repo, ledger.CodeCashClearing), Direction: ledger.Debit, Amount: 100, Currency: "USD"},
			{AccountID: mustSystemAccountID(t, ctx, repo, ledger.CodePlatformEquity), Direction: ledger.Credit, Amount: 100, Currency: "USD"},
		},
	})
	if err != nil {
		t.Fatalf("post journal: %v", err)
	}

	var postingID int64
	if err := db.QueryRowContext(ctx, "SELECT id FROM postings WHERE journal_entry_id = ? LIMIT 1", entryID).Scan(&postingID); err != nil {
		t.Fatalf("find posting: %v", err)
	}

	if _, err := db.ExecContext(ctx, "UPDATE postings SET amount = 1 WHERE id = ?", postingID); err == nil {
		t.Error("expected UPDATE on postings to be rejected by trigger")
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM postings WHERE id = ?", postingID); err == nil {
		t.Error("expected DELETE on postings to be rejected by trigger")
	}
	if _, err := db.ExecContext(ctx, "UPDATE journal_entries SET description = 'x' WHERE id = ?", entryID); err == nil {
		t.Error("expected UPDATE on journal_entries to be rejected by trigger")
	}
}

func TestPostJournalRejectsUnbalanced(t *testing.T) {
	ctx := context.Background()

	db, err := database.Open(ctx, database.DefaultConfig(testDSN(t)))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := ledger.NewRepository(db)
	_, err = repo.PostJournal(ctx, ledger.JournalEntryInput{
		EntryType:     ledger.EntryOrderAuthorized,
		ReferenceType: ledger.ReferenceTypeOrder,
		ReferenceID:   1,
		OccurredAt:    time.Now().UTC(),
		Postings: []ledger.PostingInput{
			{AccountID: mustSystemAccountID(t, ctx, repo, ledger.CodeCashClearing), Direction: ledger.Debit, Amount: 100, Currency: "USD"},
			{AccountID: mustSystemAccountID(t, ctx, repo, ledger.CodePlatformEquity), Direction: ledger.Credit, Amount: 99, Currency: "USD"},
		},
	})
	if err == nil {
		t.Fatal("expected unbalanced journal to be rejected")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mustAccount(t *testing.T, ctx context.Context, repo *ledger.Repository, code string, owner ledger.OwnerType, ownerID int64) ledger.Account {
	t.Helper()
	acc, err := repo.GetAccountByCodeOwner(ctx, code, owner, ownerID)
	if err != nil {
		t.Fatalf("get account %s/%s/%d: %v", code, owner, ownerID, err)
	}
	return acc
}

func mustSystemAccountID(t *testing.T, ctx context.Context, repo *ledger.Repository, code string) int64 {
	t.Helper()
	return mustAccount(t, ctx, repo, code, ledger.OwnerSystem, 0).ID
}

func mustBalance(t *testing.T, ctx context.Context, repo *ledger.Repository, accountID int64) ledger.Balance {
	t.Helper()
	bal, err := repo.Balance(ctx, accountID)
	if err != nil {
		t.Fatalf("balance account %d: %v", accountID, err)
	}
	return bal
}

func mustSystemBalance(t *testing.T, ctx context.Context, repo *ledger.Repository, code string) ledger.Balance {
	t.Helper()
	return mustBalance(t, ctx, repo, mustSystemAccountID(t, ctx, repo, code))
}

func TestLedgerRefundPartialThenFull(t *testing.T) {
	ctx := context.Background()

	db, err := database.Open(ctx, database.DefaultConfig(testDSN(t)))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := ledger.NewRepository(db)
	suffix := time.Now().UnixNano()

	customer, err := repo.CreateCustomer(ctx, fmt.Sprintf("rcust-%d", suffix), "Refund Test", 500000, "USD")
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	merchant, err := repo.CreateMerchant(ctx, fmt.Sprintf("rmerch-%d", suffix), "Refund Merchant", 250, "USD")
	if err != nil {
		t.Fatalf("create merchant: %v", err)
	}

	orderRef := fmt.Sprintf("rorder-%d", suffix)
	if _, err := repo.CreateOrderWithSchedule(ctx, ledger.CreateOrderParams{
		OrderRef:         orderRef,
		CustomerID:       customer.ID,
		MerchantID:       merchant.ID,
		PrincipalAmount:  10000,
		Currency:         "USD",
		InstallmentCount: 3,
		FirstDueDate:     time.Now().UTC().Truncate(time.Second),
		Interval:         14 * 24 * time.Hour,
		TraceID:          "refund-trace",
	}); err != nil {
		t.Fatalf("create order: %v", err)
	}

	receivable := mustAccount(t, ctx, repo, ledger.CodeCustomerReceivable, ledger.OwnerCustomer, customer.ID)
	payable := mustAccount(t, ctx, repo, ledger.CodeMerchantPayable, ledger.OwnerMerchant, merchant.ID)

	// Refunding more than the outstanding principal is rejected.
	if _, err := repo.RefundOrder(ctx, ledger.RefundParams{OrderRef: orderRef, Amount: 10001}); !errors.Is(err, ledger.ErrRefundExceedsOutstanding) {
		t.Fatalf("over-refund err = %v, want ErrRefundExceedsOutstanding", err)
	}

	// Partial refund of 4000: fee reversal 100, merchant reduction 3900.
	partial, err := repo.RefundOrder(ctx, ledger.RefundParams{OrderRef: orderRef, Amount: 4000, Reason: "partial"})
	if err != nil {
		t.Fatalf("partial refund: %v", err)
	}
	if partial.FeeRefunded != 100 || partial.MerchantReduction != 3900 {
		t.Errorf("partial = fee %d merchant %d, want 100 / 3900", partial.FeeRefunded, partial.MerchantReduction)
	}
	if partial.OrderStatus != "AUTHORIZED" {
		t.Errorf("partial status = %s, want AUTHORIZED", partial.OrderStatus)
	}
	if partial.RemainingOutstanding != 6000 {
		t.Errorf("partial remaining = %d, want 6000", partial.RemainingOutstanding)
	}

	if bal := mustBalance(t, ctx, repo, receivable.ID); bal.Net() != 6000 {
		t.Errorf("receivable net after partial refund = %d, want 6000", bal.Net())
	}

	// Full refund of the remaining 6000 clears fee, payable and receivable.
	full, err := repo.RefundOrder(ctx, ledger.RefundParams{OrderRef: orderRef, Amount: 6000, Reason: "rest"})
	if err != nil {
		t.Fatalf("full refund: %v", err)
	}
	if full.FeeRefunded != 150 || full.MerchantReduction != 5850 {
		t.Errorf("full = fee %d merchant %d, want 150 / 5850", full.FeeRefunded, full.MerchantReduction)
	}
	if full.OrderStatus != "REFUNDED" || full.RemainingOutstanding != 0 {
		t.Errorf("full = status %s remaining %d, want REFUNDED / 0", full.OrderStatus, full.RemainingOutstanding)
	}

	if bal := mustBalance(t, ctx, repo, receivable.ID); bal.Net() != 0 {
		t.Errorf("receivable net after full refund = %d, want 0", bal.Net())
	}
	if bal := mustBalance(t, ctx, repo, payable.ID); bal.Signed(ledger.NormalCredit) != 0 {
		t.Errorf("merchant payable after full refund = %d, want 0", bal.Signed(ledger.NormalCredit))
	}

	order, err := repo.GetOrderByRef(ctx, orderRef)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if order.Status != "REFUNDED" {
		t.Errorf("order status = %s, want REFUNDED", order.Status)
	}

	// Nothing left to refund.
	if _, err := repo.RefundOrder(ctx, ledger.RefundParams{OrderRef: orderRef, Amount: 1}); !errors.Is(err, ledger.ErrRefundExceedsOutstanding) {
		t.Fatalf("refund after full err = %v, want ErrRefundExceedsOutstanding", err)
	}
}
