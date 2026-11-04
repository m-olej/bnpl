package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/molej/bnpl/backend/internal/db/sqlc"
)

// Customer is a party who can be extended credit.
type Customer struct {
	ID          int64
	ExternalRef string
	FullName    string
	CreditLimit int64
	Currency    string
}

// Merchant is a party who receives settlements.
type Merchant struct {
	ID          int64
	MerchantRef string
	Name        string
	FeeBps      int
	Currency    string
}

// CreateCustomer inserts a customer and returns the created projection.
func (r *Repository) CreateCustomer(
	ctx context.Context,
	externalRef, fullName string,
	creditLimit int64,
	currency string,
) (Customer, error) {
	if externalRef == "" {
		return Customer{}, fmt.Errorf("%w: external_ref is required", ErrInvalidOrder)
	}
	if creditLimit < 0 {
		return Customer{}, fmt.Errorf("%w: credit_limit must be non-negative", ErrInvalidOrder)
	}
	if currency == "" {
		currency = "USD"
	}

	res, err := r.q.InsertCustomer(ctx, sqlc.InsertCustomerParams{
		ExternalRef: externalRef,
		FullName:    fullName,
		CreditLimit: creditLimit,
		Currency:    currency,
	})
	if err != nil {
		return Customer{}, fmt.Errorf("inserting customer %q: %w", externalRef, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Customer{}, fmt.Errorf("reading customer id: %w", err)
	}
	return Customer{
		ID:          id,
		ExternalRef: externalRef,
		FullName:    fullName,
		CreditLimit: creditLimit,
		Currency:    currency,
	}, nil
}

// GetCustomerByExternalRef loads a customer by its external reference.
func (r *Repository) GetCustomerByExternalRef(ctx context.Context, externalRef string) (Customer, error) {
	row, err := r.q.GetCustomerByExternalRef(ctx, externalRef)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Customer{}, fmt.Errorf("%w: ref=%s", ErrCustomerNotFound, externalRef)
		}
		return Customer{}, fmt.Errorf("loading customer %q: %w", externalRef, err)
	}
	return Customer{
		ID:          int64(row.ID),
		ExternalRef: row.ExternalRef,
		FullName:    row.FullName,
		CreditLimit: row.CreditLimit,
		Currency:    row.Currency,
	}, nil
}

// CreateMerchant inserts a merchant and returns the created projection.
func (r *Repository) CreateMerchant(
	ctx context.Context,
	merchantRef, name string,
	feeBps int,
	currency string,
) (Merchant, error) {
	if merchantRef == "" {
		return Merchant{}, fmt.Errorf("%w: merchant_ref is required", ErrInvalidOrder)
	}
	if feeBps < 0 || feeBps > 10000 {
		return Merchant{}, fmt.Errorf("%w: feeBps %d out of range [0,10000]", ErrInvalidFee, feeBps)
	}
	if currency == "" {
		currency = "USD"
	}

	res, err := r.q.InsertMerchant(ctx, sqlc.InsertMerchantParams{
		MerchantRef: merchantRef,
		Name:        name,
		FeeBps:      int32(feeBps),
		Currency:    currency,
	})
	if err != nil {
		return Merchant{}, fmt.Errorf("inserting merchant %q: %w", merchantRef, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Merchant{}, fmt.Errorf("reading merchant id: %w", err)
	}
	return Merchant{
		ID:          id,
		MerchantRef: merchantRef,
		Name:        name,
		FeeBps:      feeBps,
		Currency:    currency,
	}, nil
}
