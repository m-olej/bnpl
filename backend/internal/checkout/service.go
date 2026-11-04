// Package checkout owns order authorization and installment schedule reads.
package checkout

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/molej/bnpl/backend/internal/ledger"
)

// Service coordinates checkout use cases over the ledger repository.
type Service struct {
	repo *ledger.Repository
}

// NewService builds a checkout service.
func NewService(repo *ledger.Repository) *Service {
	return &Service{repo: repo}
}

// AuthorizeInput describes a checkout authorization request.
type AuthorizeInput struct {
	CustomerID       int64
	MerchantID       int64
	PrincipalAmount  int64
	Currency         string
	InstallmentCount int
	IntervalDays     int
	FirstDueDate     *time.Time
	IdempotencyKey   *string
	TraceID          string
}

// AuthorizeOutput is the result of an authorization.
type AuthorizeOutput struct {
	Order          ledger.Order
	Installments   []ledger.Installment
	FeeAmount      int64
	MerchantPayout int64
}

// Authorize creates an order and its installment schedule. The order reference
// is generated server-side.
func (s *Service) Authorize(ctx context.Context, in AuthorizeInput) (AuthorizeOutput, error) {
	if in.InstallmentCount < 1 {
		return AuthorizeOutput{}, fmt.Errorf("%w: installment_count must be >= 1", ledger.ErrInvalidOrder)
	}
	if in.IntervalDays < 0 {
		return AuthorizeOutput{}, fmt.Errorf("%w: interval_days must be non-negative", ledger.ErrInvalidOrder)
	}

	firstDue := time.Now().UTC()
	if in.FirstDueDate != nil {
		firstDue = in.FirstDueDate.UTC()
	}

	result, err := s.repo.CreateOrderWithSchedule(ctx, ledger.CreateOrderParams{
		OrderRef:         "ord_" + uuid.NewString(),
		CustomerID:       in.CustomerID,
		MerchantID:       in.MerchantID,
		PrincipalAmount:  in.PrincipalAmount,
		Currency:         in.Currency,
		InstallmentCount: in.InstallmentCount,
		FirstDueDate:     firstDue,
		Interval:         time.Duration(in.IntervalDays) * 24 * time.Hour,
		TraceID:          in.TraceID,
		IdempotencyKey:   in.IdempotencyKey,
	})
	if err != nil {
		return AuthorizeOutput{}, err
	}

	order, err := s.repo.GetOrder(ctx, result.OrderID)
	if err != nil {
		return AuthorizeOutput{}, err
	}
	installments, err := s.repo.ListInstallments(ctx, result.OrderID)
	if err != nil {
		return AuthorizeOutput{}, err
	}

	return AuthorizeOutput{
		Order:          order,
		Installments:   installments,
		FeeAmount:      result.FeeAmount,
		MerchantPayout: result.MerchantPayout,
	}, nil
}

// RefundInput describes a refund request against an order.
type RefundInput struct {
	OrderRef       string
	Amount         int64
	Reason         string
	IdempotencyKey *string
	TraceID        string
}

// Refund issues a (partial) refund against an order's outstanding principal.
// Amount is a positive magnitude in minor units; a refund equal to the
// remaining outstanding principal fully refunds the order.
func (s *Service) Refund(ctx context.Context, in RefundInput) (ledger.RefundResult, error) {
	return s.repo.RefundOrder(ctx, ledger.RefundParams{
		OrderRef:       in.OrderRef,
		Amount:         in.Amount,
		Reason:         in.Reason,
		TraceID:        in.TraceID,
		IdempotencyKey: in.IdempotencyKey,
	})
}

// GetOrder loads an order and its schedule by order reference.
func (s *Service) GetOrder(ctx context.Context, orderRef string) (ledger.Order, []ledger.Installment, error) {
	order, err := s.repo.GetOrderByRef(ctx, orderRef)
	if err != nil {
		return ledger.Order{}, nil, err
	}
	installments, err := s.repo.ListInstallments(ctx, order.ID)
	if err != nil {
		return ledger.Order{}, nil, err
	}
	return order, installments, nil
}

// Schedule returns just the installment schedule for an order reference.
func (s *Service) Schedule(ctx context.Context, orderRef string) ([]ledger.Installment, error) {
	order, err := s.repo.GetOrderByRef(ctx, orderRef)
	if err != nil {
		return nil, err
	}
	return s.repo.ListInstallments(ctx, order.ID)
}
