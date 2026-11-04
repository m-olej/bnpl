package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/molej/bnpl/backend/internal/db/sqlc"
)

// PayoutResult reports the effect of a merchant settlement.
type PayoutResult struct {
	OrderID        int64
	JournalEntryID int64
	PayoutAmount   int64
	TotalPaidOut   int64
}

// SettleMerchantPayout moves a merchant's earned (net-of-fee) payable into cash
// for the portion of an order that has been collected. It is safe to call
// repeatedly: the payout is always the delta between the amount earned so far
// (proportional to collected principal) and what has already been paid out, so
// worker retries cannot double-pay.
//
//  1. lock the order row;
//  2. derive collected principal and the already-settled total;
//  3. post MERCHANT_PAYOUT (debit merchant payable, credit cash clearing) for
//     the delta, if positive.
//
// A no-op (zero delta) returns a zero PayoutResult and no journal entry.
func (r *Repository) SettleMerchantPayout(
	ctx context.Context,
	orderID int64,
	occurredAt time.Time,
	traceID string,
	idempotencyKey *string,
) (PayoutResult, error) {
	if orderID <= 0 {
		return PayoutResult{}, fmt.Errorf("%w: order_id must be positive", ErrInvalidOrder)
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	var result PayoutResult
	err := r.tx.WithTx(ctx, func(tx *sql.Tx) error {
		q := r.q.WithTx(tx)

		order, err := q.GetOrderByIDForUpdate(ctx, uint64(orderID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: id=%d", ErrOrderNotFound, orderID)
			}
			return fmt.Errorf("locking order %d: %w", orderID, err)
		}

		collected, err := q.SumCollectedInstallments(ctx, order.ID)
		if err != nil {
			return fmt.Errorf("summing collected installments for order %d: %w", order.ID, err)
		}

		// Net merchant share earned so far, proportional to collected principal.
		// Floors; the final installment trues up because collected == principal
		// yields exactly principal-fee.
		target, err := mulDiv(collected, order.PrincipalAmount-order.FeeAmount, order.PrincipalAmount)
		if err != nil {
			return err
		}

		payable, err := q.GetAccountByCodeOwner(ctx, sqlc.GetAccountByCodeOwnerParams{
			Code:      CodeMerchantPayable,
			OwnerType: sqlc.AccountsOwnerTypeMERCHANT,
			OwnerID:   order.MerchantID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: merchant payable for merchant %d", ErrAccountNotFound, order.MerchantID)
			}
			return fmt.Errorf("loading merchant payable: %w", err)
		}

		alreadyPaidOut, err := q.SumOrderPayoutPostings(ctx, sqlc.SumOrderPayoutPostingsParams{
			OrderID:          order.ID,
			PayableAccountID: payable.ID,
		})
		if err != nil {
			return fmt.Errorf("summing prior payouts for order %d: %w", order.ID, err)
		}

		payout := target - alreadyPaidOut
		if payout <= 0 {
			result = PayoutResult{OrderID: int64(order.ID), TotalPaidOut: alreadyPaidOut}
			return nil
		}

		cashClearingID, err := systemAccountID(ctx, q, CodeCashClearing)
		if err != nil {
			return err
		}

		journalID, err := postJournal(ctx, q, JournalEntryInput{
			EntryType:      EntryMerchantPayout,
			ReferenceType:  ReferenceTypeOrder,
			ReferenceID:    int64(order.ID),
			Description:    fmt.Sprintf("settle merchant payout for order %s", order.OrderRef),
			TraceID:        traceID,
			IdempotencyKey: idempotencyKey,
			OccurredAt:     occurredAt,
			Postings: []PostingInput{
				{AccountID: int64(payable.ID), Direction: Debit, Amount: payout, Currency: order.Currency},
				{AccountID: cashClearingID, Direction: Credit, Amount: payout, Currency: order.Currency},
			},
		})
		if err != nil {
			return err
		}

		result = PayoutResult{
			OrderID:        int64(order.ID),
			JournalEntryID: journalID,
			PayoutAmount:   payout,
			TotalPaidOut:   alreadyPaidOut + payout,
		}
		return nil
	})
	if err != nil {
		return PayoutResult{}, err
	}
	return result, nil
}
