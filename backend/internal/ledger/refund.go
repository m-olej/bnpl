package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/molej/bnpl/backend/internal/db/sqlc"
)

// RefundParams describes a refund request against an authorized order.
//
// Refunds only reduce the order's outstanding (uncollected, unrefunded)
// principal. Already-collected installments represent cash the customer has
// paid and are outside the scope of this compensating entry; there is no cash
// disbursement path yet.
type RefundParams struct {
	OrderRef       string
	Amount         int64
	Reason         string
	TraceID        string
	IdempotencyKey *string
	OccurredAt     time.Time
}

// RefundResult reports the effect of a refund.
type RefundResult struct {
	OrderID              int64
	JournalEntryID       int64
	RefundedAmount       int64
	FeeRefunded          int64
	MerchantReduction    int64
	OrderStatus          string
	RemainingOutstanding int64
}

// RefundOrder posts a compensating REFUND journal entry for an order and
// advances the order to REFUNDED when nothing remains outstanding:
//
//  1. lock the order row (and therefore the refund race for that order);
//  2. derive the outstanding principal from collected installments and prior
//     refund postings;
//  3. apportion the refund between the merchant payable and fee revenue so the
//     journal entry stays balanced;
//  4. post the REFUND entry (credit customer receivable, debit merchant
//     payable, debit fee revenue).
//
// The entire operation is atomic: a failure at any step rolls back everything.
func (r *Repository) RefundOrder(ctx context.Context, p RefundParams) (RefundResult, error) {
	if p.OrderRef == "" {
		return RefundResult{}, fmt.Errorf("%w: order_ref is required", ErrInvalidRefund)
	}
	if p.Amount <= 0 {
		return RefundResult{}, fmt.Errorf("%w: amount %d must be positive", ErrInvalidRefund, p.Amount)
	}
	if p.OccurredAt.IsZero() {
		p.OccurredAt = time.Now().UTC()
	}

	var result RefundResult
	err := r.tx.WithTx(ctx, func(tx *sql.Tx) error {
		q := r.q.WithTx(tx)

		order, err := q.GetOrderByRefForUpdate(ctx, p.OrderRef)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: ref=%s", ErrOrderNotFound, p.OrderRef)
			}
			return fmt.Errorf("locking order %q: %w", p.OrderRef, err)
		}

		collected, err := q.SumCollectedInstallments(ctx, order.ID)
		if err != nil {
			return fmt.Errorf("summing collected installments for order %d: %w", order.ID, err)
		}

		receivable, err := q.GetAccountByCodeOwner(ctx, sqlc.GetAccountByCodeOwnerParams{
			Code:      CodeCustomerReceivable,
			OwnerType: sqlc.AccountsOwnerTypeCUSTOMER,
			OwnerID:   order.CustomerID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: customer receivable for customer %d", ErrAccountNotFound, order.CustomerID)
			}
			return fmt.Errorf("loading customer receivable: %w", err)
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
		feeAccountID, err := systemAccountID(ctx, q, CodeMerchantFeeRevenue)
		if err != nil {
			return err
		}

		prior, err := q.SumOrderRefundPostings(ctx, sqlc.SumOrderRefundPostingsParams{
			ReceivableAccountID: receivable.ID,
			FeeAccountID:        uint64(feeAccountID),
			OrderID:             order.ID,
		})
		if err != nil {
			return fmt.Errorf("summing prior refunds for order %d: %w", order.ID, err)
		}

		outstanding := order.PrincipalAmount - collected - prior.RefundedPrincipal
		if outstanding < 0 {
			outstanding = 0
		}
		if p.Amount > outstanding {
			return fmt.Errorf("%w: order=%s requested=%d outstanding=%d",
				ErrRefundExceedsOutstanding, p.OrderRef, p.Amount, outstanding)
		}

		feeRefund, err := refundFee(order.FeeAmount, prior.RefundedFee, p.Amount, outstanding)
		if err != nil {
			return err
		}
		merchantReduction := p.Amount - feeRefund

		postings := []PostingInput{
			{AccountID: int64(receivable.ID), Direction: Credit, Amount: p.Amount, Currency: order.Currency},
		}
		if merchantReduction > 0 {
			postings = append(postings, PostingInput{
				AccountID: int64(payable.ID), Direction: Debit, Amount: merchantReduction, Currency: order.Currency,
			})
		}
		if feeRefund > 0 {
			postings = append(postings, PostingInput{
				AccountID: feeAccountID, Direction: Debit, Amount: feeRefund, Currency: order.Currency,
			})
		}

		desc := fmt.Sprintf("refund order %s", p.OrderRef)
		if p.Reason != "" {
			desc = fmt.Sprintf("%s: %s", desc, p.Reason)
		}

		journalID, err := postJournal(ctx, q, JournalEntryInput{
			EntryType:      EntryRefund,
			ReferenceType:  ReferenceTypeOrder,
			ReferenceID:    int64(order.ID),
			Description:    desc,
			TraceID:        p.TraceID,
			IdempotencyKey: p.IdempotencyKey,
			OccurredAt:     p.OccurredAt,
			Postings:       postings,
		})
		if err != nil {
			return err
		}

		// Keep the pending schedule consistent with the reduced outstanding
		// principal: cancel/reduce tail installments so the worker cannot
		// collect a refunded portion.
		if err := reconcileScheduleAfterRefund(ctx, q, order.ID, p.Amount); err != nil {
			return err
		}

		remaining := outstanding - p.Amount
		status := order.Status
		if remaining == 0 {
			status = sqlc.OrdersStatusREFUNDED
			if err := q.UpdateOrderStatus(ctx, sqlc.UpdateOrderStatusParams{
				Status: status,
				ID:     order.ID,
			}); err != nil {
				return fmt.Errorf("marking order %d refunded: %w", order.ID, err)
			}
		}

		result = RefundResult{
			OrderID:              int64(order.ID),
			JournalEntryID:       journalID,
			RefundedAmount:       p.Amount,
			FeeRefunded:          feeRefund,
			MerchantReduction:    merchantReduction,
			OrderStatus:          string(status),
			RemainingOutstanding: remaining,
		}
		return nil
	})
	if err != nil {
		return RefundResult{}, err
	}
	return result, nil
}

// reconcileScheduleAfterRefund keeps the pending installment schedule aligned
// with a refund that reduced the order's outstanding principal. It walks the
// installments from the tail (newest first), cancelling whole installments and
// reducing the boundary one, until the refunded amount is absorbed. Already
// collected (PAID) installments are never touched.
func reconcileScheduleAfterRefund(ctx context.Context, q *sqlc.Queries, orderID uint64, amount int64) error {
	installments, err := q.ListInstallmentsByOrder(ctx, orderID)
	if err != nil {
		return fmt.Errorf("listing installments for order %d: %w", orderID, err)
	}

	remaining := amount
	for i := len(installments) - 1; i >= 0 && remaining > 0; i-- {
		inst := installments[i]
		switch inst.Status {
		case sqlc.InstallmentsStatusSCHEDULED, sqlc.InstallmentsStatusPROCESSING:
		default:
			continue
		}
		if inst.Amount <= remaining {
			if err := q.CancelInstallment(ctx, inst.ID); err != nil {
				return fmt.Errorf("cancelling installment %d: %w", inst.ID, err)
			}
			remaining -= inst.Amount
			continue
		}
		if err := q.UpdateInstallmentAmount(ctx, sqlc.UpdateInstallmentAmountParams{
			Amount: inst.Amount - remaining,
			ID:     inst.ID,
		}); err != nil {
			return fmt.Errorf("reducing installment %d: %w", inst.ID, err)
		}
		remaining = 0
	}
	if remaining > 0 {
		return fmt.Errorf("%w: could not apply %d of refund to order %d schedule",
			ErrInvalidRefund, remaining, orderID)
	}
	return nil
}

// refundFee computes how much recognized platform fee to reverse for a refund
// of `amount` against `outstanding` principal, given the fee already reversed
// (refundedFee) and the order's total fee.
//
// The reversal is proportional to the outstanding principal still on the
// books. A full refund of the remaining principal reverses the remaining fee
// exactly. The result is clamped to [0, amount] so the balancing merchant
// payable reduction stays non-negative; without the clamp a heavily collected
// order could recognize more fee reversal than principal being refunded.
func refundFee(totalFee, refundedFee, amount, outstanding int64) (int64, error) {
	if outstanding <= 0 {
		return 0, fmt.Errorf("%w: nothing outstanding", ErrRefundExceedsOutstanding)
	}
	remainingFee := totalFee - refundedFee
	if remainingFee <= 0 {
		return 0, nil
	}
	if amount >= outstanding {
		remainingFee = min(remainingFee, amount)
		return remainingFee, nil
	}
	feeRefund, err := mulDiv(remainingFee, amount, outstanding)
	if err != nil {
		return 0, err
	}
	return min(feeRefund, amount), nil
}
