package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/molej/bnpl/backend/internal/db/sqlc"
)

// Installment is a repayment schedule row projection.
type Installment struct {
	ID           int64
	OrderID      int64
	CustomerID   int64
	Number       int
	Amount       int64
	Currency     string
	DueDate      time.Time
	Status       string
	AttemptCount int
	PaidAt       *time.Time
}

// PayInstallmentResult reports the effect of collecting one installment.
type PayInstallmentResult struct {
	InstallmentID  int64
	OrderID        int64
	JournalEntryID int64
	OrderStatus    string
}

// PayInstallment captures a single installment. It locks the installment and
// its order, posts an INSTALLMENT_PAID journal entry (debit cash clearing,
// credit customer receivable), marks the installment PAID, and advances the
// order to ACTIVE or COMPLETED.
func (r *Repository) PayInstallment(
	ctx context.Context,
	installmentID int64,
	occurredAt time.Time,
	traceID string,
	idempotencyKey *string,
) (PayInstallmentResult, error) {
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	var result PayInstallmentResult
	err := r.tx.WithTx(ctx, func(tx *sql.Tx) error {
		q := r.q.WithTx(tx)

		inst, err := q.GetInstallmentByIDForUpdate(ctx, uint64(installmentID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: id=%d", ErrInstallmentNotFound, installmentID)
			}
			return fmt.Errorf("locking installment %d: %w", installmentID, err)
		}

		switch inst.Status {
		case sqlc.InstallmentsStatusSCHEDULED, sqlc.InstallmentsStatusPROCESSING:
			// payable
		default:
			return fmt.Errorf("%w: id=%d status=%s", ErrInstallmentNotPayable, installmentID, inst.Status)
		}

		if _, err := q.GetOrderByIDForUpdate(ctx, inst.OrderID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: id=%d", ErrOrderNotFound, inst.OrderID)
			}
			return fmt.Errorf("locking order %d: %w", inst.OrderID, err)
		}

		cashClearingID, err := systemAccountID(ctx, q, CodeCashClearing)
		if err != nil {
			return err
		}
		receivable, err := q.GetAccountByCodeOwner(ctx, sqlc.GetAccountByCodeOwnerParams{
			Code:      CodeCustomerReceivable,
			OwnerType: sqlc.AccountsOwnerTypeCUSTOMER,
			OwnerID:   inst.CustomerID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: customer receivable for customer %d", ErrAccountNotFound, inst.CustomerID)
			}
			return fmt.Errorf("loading customer receivable: %w", err)
		}

		journalID, err := postJournal(ctx, q, JournalEntryInput{
			EntryType:      EntryInstallmentPaid,
			ReferenceType:  ReferenceTypeInstallment,
			ReferenceID:    int64(inst.ID),
			Description:    fmt.Sprintf("collect installment %d of order %d", inst.InstallmentNo, inst.OrderID),
			TraceID:        traceID,
			IdempotencyKey: idempotencyKey,
			OccurredAt:     occurredAt,
			Postings: []PostingInput{
				{AccountID: cashClearingID, Direction: Debit, Amount: inst.Amount, Currency: inst.Currency},
				{AccountID: int64(receivable.ID), Direction: Credit, Amount: inst.Amount, Currency: inst.Currency},
			},
		})
		if err != nil {
			return err
		}

		if err := q.MarkInstallmentPaid(ctx, sqlc.MarkInstallmentPaidParams{
			PaidAt:         sql.NullTime{Time: occurredAt, Valid: true},
			JournalEntryID: sql.NullInt64{Int64: journalID, Valid: true},
			ID:             inst.ID,
		}); err != nil {
			return fmt.Errorf("marking installment %d paid: %w", installmentID, err)
		}

		nextStatus, err := r.advanceOrderStatus(ctx, q, inst.OrderID)
		if err != nil {
			return err
		}

		result = PayInstallmentResult{
			InstallmentID:  int64(inst.ID),
			OrderID:        int64(inst.OrderID),
			JournalEntryID: journalID,
			OrderStatus:    nextStatus,
		}
		return nil
	})
	if err != nil {
		return PayInstallmentResult{}, err
	}
	return result, nil
}

// advanceOrderStatus moves the order to COMPLETED when every installment is
// PAID, otherwise to ACTIVE. It returns the resulting status.
func (r *Repository) advanceOrderStatus(ctx context.Context, q *sqlc.Queries, orderID uint64) (string, error) {
	installments, err := q.ListInstallmentsByOrder(ctx, orderID)
	if err != nil {
		return "", fmt.Errorf("listing installments for order %d: %w", orderID, err)
	}

	allPaid := true
	for _, inst := range installments {
		if inst.Status != sqlc.InstallmentsStatusPAID {
			allPaid = false
			break
		}
	}

	next := sqlc.OrdersStatusACTIVE
	if allPaid {
		next = sqlc.OrdersStatusCOMPLETED
	}
	if err := q.UpdateOrderStatus(ctx, sqlc.UpdateOrderStatusParams{
		Status: next,
		ID:     orderID,
	}); err != nil {
		return "", fmt.Errorf("advancing order %d status: %w", orderID, err)
	}
	return string(next), nil
}

// ClaimDueInstallments atomically selects due installments with
// SELECT ... FOR UPDATE SKIP LOCKED, marks them PROCESSING and commits,
// releasing row locks before any external gateway call (docs/0003).
func (r *Repository) ClaimDueInstallments(ctx context.Context, asOf time.Time, limit int32) ([]Installment, error) {
	var claimed []Installment
	err := r.tx.WithTx(ctx, func(tx *sql.Tx) error {
		q := r.q.WithTx(tx)

		rows, err := q.ClaimDueInstallments(ctx, sqlc.ClaimDueInstallmentsParams{
			DueDate: asOf,
			Limit:   limit,
		})
		if err != nil {
			return fmt.Errorf("claiming due installments: %w", err)
		}

		claimed = make([]Installment, 0, len(rows))
		for i := range rows {
			if err := q.MarkInstallmentProcessing(ctx, rows[i].ID); err != nil {
				return fmt.Errorf("marking installment %d processing: %w", rows[i].ID, err)
			}
			inst := mapInstallment(rows[i])
			inst.Status = string(sqlc.InstallmentsStatusPROCESSING)
			claimed = append(claimed, inst)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// ListInstallments returns an order's schedule ordered by installment number.
func (r *Repository) ListInstallments(ctx context.Context, orderID int64) ([]Installment, error) {
	rows, err := r.q.ListInstallmentsByOrder(ctx, uint64(orderID))
	if err != nil {
		return nil, fmt.Errorf("listing installments for order %d: %w", orderID, err)
	}
	out := make([]Installment, 0, len(rows))
	for i := range rows {
		out = append(out, mapInstallment(rows[i]))
	}
	return out, nil
}

func mapInstallment(i sqlc.Installment) Installment {
	inst := Installment{
		ID:           int64(i.ID),
		OrderID:      int64(i.OrderID),
		CustomerID:   int64(i.CustomerID),
		Number:       int(i.InstallmentNo),
		Amount:       i.Amount,
		Currency:     i.Currency,
		DueDate:      i.DueDate,
		Status:       string(i.Status),
		AttemptCount: int(i.AttemptCount),
	}
	if i.PaidAt.Valid {
		t := i.PaidAt.Time
		inst.PaidAt = &t
	}
	return inst
}
