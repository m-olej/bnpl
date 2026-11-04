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
	return mapCustomer(row), nil
}

// GetCustomer loads a customer by id.
func (r *Repository) GetCustomer(ctx context.Context, customerID int64) (Customer, error) {
	row, err := r.q.GetCustomerByID(ctx, uint64(customerID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Customer{}, fmt.Errorf("%w: id=%d", ErrCustomerNotFound, customerID)
		}
		return Customer{}, fmt.Errorf("loading customer %d: %w", customerID, err)
	}
	return mapCustomer(row), nil
}

// AvailableCredit returns credit_limit minus the customer's outstanding
// receivable, clamped at zero.
func (r *Repository) AvailableCredit(ctx context.Context, customerID int64) (int64, error) {
	cust, err := r.q.GetCustomerByID(ctx, uint64(customerID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("%w: id=%d", ErrCustomerNotFound, customerID)
		}
		return 0, fmt.Errorf("loading customer %d: %w", customerID, err)
	}

	outstanding, err := outstandingReceivable(ctx, r.q, customerID)
	if err != nil {
		return 0, err
	}
	available := cust.CreditLimit - outstanding
	if available < 0 {
		return 0, nil
	}
	return available, nil
}

// SetCustomerCreditLimit updates a customer's credit limit under a pessimistic
// lock (AGENTS.md section 1.3) and returns the updated projection.
func (r *Repository) SetCustomerCreditLimit(ctx context.Context, customerID, limit int64) (Customer, error) {
	if limit < 0 {
		return Customer{}, fmt.Errorf("%w: credit_limit must be non-negative", ErrInvalidOrder)
	}

	var updated Customer
	err := r.tx.WithTx(ctx, func(tx *sql.Tx) error {
		q := r.q.WithTx(tx)
		cust, err := q.GetCustomerByIDForUpdate(ctx, uint64(customerID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: id=%d", ErrCustomerNotFound, customerID)
			}
			return fmt.Errorf("locking customer %d: %w", customerID, err)
		}
		if err := q.UpdateCustomerCreditLimit(ctx, sqlc.UpdateCustomerCreditLimitParams{
			CreditLimit: limit,
			ID:          cust.ID,
		}); err != nil {
			return fmt.Errorf("updating credit limit for customer %d: %w", customerID, err)
		}
		cust.CreditLimit = limit
		updated = mapCustomer(cust)
		return nil
	})
	if err != nil {
		return Customer{}, err
	}
	return updated, nil
}

// GetMerchant loads a merchant by id.
func (r *Repository) GetMerchant(ctx context.Context, merchantID int64) (Merchant, error) {
	row, err := r.q.GetMerchantByID(ctx, uint64(merchantID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Merchant{}, fmt.Errorf("%w: id=%d", ErrMerchantNotFound, merchantID)
		}
		return Merchant{}, fmt.Errorf("loading merchant %d: %w", merchantID, err)
	}
	return mapMerchant(row), nil
}

// GetMerchantByRef loads a merchant by its external reference.
func (r *Repository) GetMerchantByRef(ctx context.Context, merchantRef string) (Merchant, error) {
	row, err := r.q.GetMerchantByRef(ctx, merchantRef)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Merchant{}, fmt.Errorf("%w: ref=%s", ErrMerchantNotFound, merchantRef)
		}
		return Merchant{}, fmt.Errorf("loading merchant %q: %w", merchantRef, err)
	}
	return mapMerchant(row), nil
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

func mapCustomer(c sqlc.Customer) Customer {
	return Customer{
		ID:          int64(c.ID),
		ExternalRef: c.ExternalRef,
		FullName:    c.FullName,
		CreditLimit: c.CreditLimit,
		Currency:    c.Currency,
	}
}

func mapMerchant(m sqlc.Merchant) Merchant {
	return Merchant{
		ID:          int64(m.ID),
		MerchantRef: m.MerchantRef,
		Name:        m.Name,
		FeeBps:      int(m.FeeBps),
		Currency:    m.Currency,
	}
}
