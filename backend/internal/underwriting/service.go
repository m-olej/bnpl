// Package underwriting owns customer/merchant onboarding and credit eligibility.
package underwriting

import (
	"context"
	"fmt"

	"github.com/molej/bnpl/backend/internal/ledger"
)

// Service coordinates underwriting use cases over the ledger repository.
type Service struct {
	repo *ledger.Repository
}

// NewService builds an underwriting service.
func NewService(repo *ledger.Repository) *Service {
	return &Service{repo: repo}
}

// CreateCustomerInput describes an onboarding request.
type CreateCustomerInput struct {
	ExternalRef string
	FullName    string
	CreditLimit int64
	Currency    string
}

// CreateCustomer onboards a customer.
func (s *Service) CreateCustomer(ctx context.Context, in CreateCustomerInput) (ledger.Customer, error) {
	return s.repo.CreateCustomer(ctx, in.ExternalRef, in.FullName, in.CreditLimit, in.Currency)
}

// GetCustomerByRef loads a customer.
func (s *Service) GetCustomerByRef(ctx context.Context, externalRef string) (ledger.Customer, error) {
	return s.repo.GetCustomerByExternalRef(ctx, externalRef)
}

// SetLimitByRef updates a customer's credit limit (identified by reference).
func (s *Service) SetLimitByRef(ctx context.Context, externalRef string, limit int64) (ledger.Customer, error) {
	cust, err := s.repo.GetCustomerByExternalRef(ctx, externalRef)
	if err != nil {
		return ledger.Customer{}, err
	}
	return s.repo.SetCustomerCreditLimit(ctx, cust.ID, limit)
}

// CreditView summarises a customer's credit position.
type CreditView struct {
	Customer    ledger.Customer
	Outstanding int64
	Available   int64
}

// Credit returns the credit position for a customer.
func (s *Service) Credit(ctx context.Context, externalRef string) (CreditView, error) {
	cust, err := s.repo.GetCustomerByExternalRef(ctx, externalRef)
	if err != nil {
		return CreditView{}, err
	}
	available, err := s.repo.AvailableCredit(ctx, cust.ID)
	if err != nil {
		return CreditView{}, err
	}
	outstanding := cust.CreditLimit - available
	if outstanding < 0 {
		outstanding = 0
	}
	return CreditView{Customer: cust, Outstanding: outstanding, Available: available}, nil
}

// CreateMerchantInput describes merchant onboarding.
type CreateMerchantInput struct {
	MerchantRef string
	Name        string
	FeeBps      int
	Currency    string
}

// CreateMerchant onboards a merchant.
func (s *Service) CreateMerchant(ctx context.Context, in CreateMerchantInput) (ledger.Merchant, error) {
	return s.repo.CreateMerchant(ctx, in.MerchantRef, in.Name, in.FeeBps, in.Currency)
}

// GetMerchantByRef loads a merchant.
func (s *Service) GetMerchantByRef(ctx context.Context, merchantRef string) (ledger.Merchant, error) {
	return s.repo.GetMerchantByRef(ctx, merchantRef)
}

// Decision is an underwriting eligibility result.
type Decision struct {
	Approved  bool
	Requested int64
	Available int64
	Reason    string
}

// Evaluate decides whether a requested amount fits a customer's available
// credit. The authoritative enforcement still happens inside the order
// transaction (see ledger.CreateOrderWithSchedule).
func (s *Service) Evaluate(ctx context.Context, externalRef string, amount int64) (Decision, error) {
	if amount <= 0 {
		return Decision{}, fmt.Errorf("%w: amount must be positive", ledger.ErrInvalidOrder)
	}
	view, err := s.Credit(ctx, externalRef)
	if err != nil {
		return Decision{}, err
	}

	decision := Decision{Requested: amount, Available: view.Available}
	if amount <= view.Available {
		decision.Approved = true
		decision.Reason = "within available credit"
	} else {
		decision.Reason = fmt.Sprintf("requested %d exceeds available credit %d", amount, view.Available)
	}
	return decision, nil
}
