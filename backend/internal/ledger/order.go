package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/molej/bnpl/backend/internal/db/sqlc"
)

// CreateOrderParams describes an authorization request.
type CreateOrderParams struct {
	OrderRef         string
	CustomerID       int64
	MerchantID       int64
	PrincipalAmount  int64
	Currency         string
	InstallmentCount int
	FirstDueDate     time.Time
	Interval         time.Duration
	TraceID          string
	IdempotencyKey   *string
	OccurredAt       time.Time
}

// ScheduledInstallment is a persisted installment row projection.
type ScheduledInstallment struct {
	ID      int64
	Number  int
	Amount  int64
	DueDate time.Time
}

// OrderResult is returned after a successful authorization.
type OrderResult struct {
	OrderID        int64
	JournalEntryID int64
	FeeAmount      int64
	MerchantPayout int64
	Installments   []ScheduledInstallment
}

// Order is the checkout intent projection.
type Order struct {
	ID               int64
	OrderRef         string
	CustomerID       int64
	MerchantID       int64
	PrincipalAmount  int64
	Currency         string
	InstallmentCount int
	FeeAmount        int64
	Status           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// CreateOrderWithSchedule authorizes an order and schedules its installments in
// a single transaction:
//
//  1. lock the customer and merchant rows;
//  2. compute the platform fee and per-party sub-ledger accounts;
//  3. insert the order;
//  4. post the ORDER_AUTHORIZED journal entry (debit customer receivable,
//     credit merchant payable net of fee, credit fee revenue);
//  5. insert the deterministic installment schedule.
//
// The entire operation is atomic: a failure at any step rolls back everything.
func (r *Repository) CreateOrderWithSchedule(ctx context.Context, p CreateOrderParams) (OrderResult, error) {
	if err := validateCreateOrder(p); err != nil {
		return OrderResult{}, err
	}

	plans, err := BuildSchedule(p.PrincipalAmount, p.InstallmentCount, p.FirstDueDate, p.Interval)
	if err != nil {
		return OrderResult{}, err
	}

	var result OrderResult
	err = r.tx.WithTx(ctx, func(tx *sql.Tx) error {
		q := r.q.WithTx(tx)

		customer, err := q.GetCustomerByIDForUpdate(ctx, uint64(p.CustomerID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: id=%d", ErrCustomerNotFound, p.CustomerID)
			}
			return fmt.Errorf("locking customer %d: %w", p.CustomerID, err)
		}

		merchant, err := q.GetMerchantByIDForUpdate(ctx, uint64(p.MerchantID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: id=%d", ErrMerchantNotFound, p.MerchantID)
			}
			return fmt.Errorf("locking merchant %d: %w", p.MerchantID, err)
		}

		currency := p.Currency
		if currency == "" {
			currency = customer.Currency
		}

		fee, err := CalculateFee(p.PrincipalAmount, int64(merchant.FeeBps))
		if err != nil {
			return err
		}
		if fee >= p.PrincipalAmount {
			return fmt.Errorf("%w: fee %d consumes principal %d", ErrInvalidFee, fee, p.PrincipalAmount)
		}
		merchantPayout := p.PrincipalAmount - fee

		receivableID, err := ensureAccount(ctx, q, CodeCustomerReceivable, "Customer Receivable",
			AccountTypeAsset, NormalDebit, OwnerCustomer, p.CustomerID, currency)
		if err != nil {
			return err
		}
		payableID, err := ensureAccount(ctx, q, CodeMerchantPayable, "Merchant Payable",
			AccountTypeLiability, NormalCredit, OwnerMerchant, p.MerchantID, currency)
		if err != nil {
			return err
		}
		feeRevenueID, err := systemAccountID(ctx, q, CodeMerchantFeeRevenue)
		if err != nil {
			return err
		}

		res, err := q.InsertOrder(ctx, sqlc.InsertOrderParams{
			OrderRef:         p.OrderRef,
			CustomerID:       uint64(p.CustomerID),
			MerchantID:       uint64(p.MerchantID),
			PrincipalAmount:  p.PrincipalAmount,
			Currency:         currency,
			InstallmentCount: uint8(p.InstallmentCount),
			FeeAmount:        fee,
			Status:           sqlc.OrdersStatusPENDING,
			IdempotencyKey:   nullString(p.IdempotencyKey),
		})
		if err != nil {
			return fmt.Errorf("inserting order %q: %w", p.OrderRef, err)
		}
		orderID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("reading order id: %w", err)
		}

		postings := []PostingInput{
			{AccountID: receivableID, Direction: Debit, Amount: p.PrincipalAmount, Currency: currency},
			{AccountID: payableID, Direction: Credit, Amount: merchantPayout, Currency: currency},
		}
		if fee > 0 {
			postings = append(postings, PostingInput{
				AccountID: feeRevenueID, Direction: Credit, Amount: fee, Currency: currency,
			})
		}

		journalID, err := postJournal(ctx, q, JournalEntryInput{
			EntryType:      EntryOrderAuthorized,
			ReferenceType:  ReferenceTypeOrder,
			ReferenceID:    orderID,
			Description:    fmt.Sprintf("authorize order %s", p.OrderRef),
			TraceID:        p.TraceID,
			IdempotencyKey: p.IdempotencyKey,
			OccurredAt:     p.OccurredAt,
			Postings:       postings,
		})
		if err != nil {
			return err
		}

		if err := q.UpdateOrderStatus(ctx, sqlc.UpdateOrderStatusParams{
			Status: sqlc.OrdersStatusAUTHORIZED,
			ID:     uint64(orderID),
		}); err != nil {
			return fmt.Errorf("marking order %d authorized: %w", orderID, err)
		}

		installments := make([]ScheduledInstallment, 0, len(plans))
		for _, plan := range plans {
			instRes, err := q.InsertInstallment(ctx, sqlc.InsertInstallmentParams{
				OrderID:       uint64(orderID),
				CustomerID:    uint64(p.CustomerID),
				InstallmentNo: uint16(plan.Number),
				Amount:        plan.Amount,
				Currency:      currency,
				DueDate:       plan.DueDate,
				Status:        sqlc.InstallmentsStatusSCHEDULED,
			})
			if err != nil {
				return fmt.Errorf("inserting installment %d: %w", plan.Number, err)
			}
			instID, err := instRes.LastInsertId()
			if err != nil {
				return fmt.Errorf("reading installment %d id: %w", plan.Number, err)
			}
			installments = append(installments, ScheduledInstallment{
				ID:      instID,
				Number:  plan.Number,
				Amount:  plan.Amount,
				DueDate: plan.DueDate,
			})
		}

		result = OrderResult{
			OrderID:        orderID,
			JournalEntryID: journalID,
			FeeAmount:      fee,
			MerchantPayout: merchantPayout,
			Installments:   installments,
		}
		return nil
	})
	if err != nil {
		return OrderResult{}, err
	}
	return result, nil
}

// GetOrder loads an order by id.
func (r *Repository) GetOrder(ctx context.Context, orderID int64) (Order, error) {
	row, err := r.q.GetOrderByID(ctx, uint64(orderID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Order{}, fmt.Errorf("%w: id=%d", ErrOrderNotFound, orderID)
		}
		return Order{}, fmt.Errorf("loading order %d: %w", orderID, err)
	}
	return mapOrder(row), nil
}

// GetOrderByRef loads an order by its external reference.
func (r *Repository) GetOrderByRef(ctx context.Context, orderRef string) (Order, error) {
	row, err := r.q.GetOrderByRef(ctx, orderRef)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Order{}, fmt.Errorf("%w: ref=%s", ErrOrderNotFound, orderRef)
		}
		return Order{}, fmt.Errorf("loading order %q: %w", orderRef, err)
	}
	return mapOrder(row), nil
}

func validateCreateOrder(p CreateOrderParams) error {
	switch {
	case p.OrderRef == "":
		return fmt.Errorf("%w: order_ref is required", ErrInvalidOrder)
	case p.CustomerID <= 0:
		return fmt.Errorf("%w: customer_id must be positive", ErrInvalidOrder)
	case p.MerchantID <= 0:
		return fmt.Errorf("%w: merchant_id must be positive", ErrInvalidOrder)
	case p.PrincipalAmount <= 0:
		return fmt.Errorf("%w: principal_amount must be positive", ErrInvalidOrder)
	case p.InstallmentCount < 1:
		return fmt.Errorf("%w: installment_count must be >= 1", ErrInvalidOrder)
	case p.InstallmentCount > 255:
		return fmt.Errorf("%w: installment_count %d exceeds 255", ErrInvalidOrder, p.InstallmentCount)
	case p.Currency != "" && len(p.Currency) != 3:
		return fmt.Errorf("%w: currency %q must be a 3-letter code", ErrInvalidOrder, p.Currency)
	}
	return nil
}

func mapOrder(o sqlc.Order) Order {
	return Order{
		ID:               int64(o.ID),
		OrderRef:         o.OrderRef,
		CustomerID:       int64(o.CustomerID),
		MerchantID:       int64(o.MerchantID),
		PrincipalAmount:  o.PrincipalAmount,
		Currency:         o.Currency,
		InstallmentCount: int(o.InstallmentCount),
		FeeAmount:        o.FeeAmount,
		Status:           string(o.Status),
		CreatedAt:        o.CreatedAt,
		UpdatedAt:        o.UpdatedAt,
	}
}
