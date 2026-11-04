// Package ledger implements the immutable, double-entry financial core of the
// BNPL platform. See AGENTS.md and docs/001 for the invariants this package
// upholds.
package ledger

import "errors"

// Domain sentinel errors. Transport/database errors are wrapped around these
// with fmt.Errorf("...: %w", err) so callers can discriminate with errors.Is.
var (
	// ErrUnbalancedJournal is returned when sum(debits) != sum(credits).
	ErrUnbalancedJournal = errors.New("ledger: journal entry is unbalanced")
	// ErrTooFewPostings is returned when a journal entry has fewer than two
	// postings; double-entry requires at least a debit and a credit.
	ErrTooFewPostings = errors.New("ledger: journal entry requires at least two postings")
	// ErrNonPositiveAmount is returned for a posting whose magnitude is <= 0.
	// The direction field carries the sign, so magnitudes must be positive.
	ErrNonPositiveAmount = errors.New("ledger: posting amount must be positive")
	// ErrUnknownDirection is returned for a direction other than DEBIT/CREDIT.
	ErrUnknownDirection = errors.New("ledger: unknown posting direction")
	// ErrMixedCurrency is returned when postings within one journal entry do
	// not share a single currency.
	ErrMixedCurrency = errors.New("ledger: postings must share a single currency")
	// ErrAmountOverflow is returned when summing amounts would overflow int64.
	ErrAmountOverflow = errors.New("ledger: amount sum overflows int64")

	// ErrAccountNotFound is returned when a referenced account row is absent.
	ErrAccountNotFound = errors.New("ledger: account not found")
	// ErrOrderNotFound is returned when a referenced order row is absent.
	ErrOrderNotFound = errors.New("ledger: order not found")
	// ErrCustomerNotFound is returned when a referenced customer row is absent.
	ErrCustomerNotFound = errors.New("ledger: customer not found")
	// ErrMerchantNotFound is returned when a referenced merchant row is absent.
	ErrMerchantNotFound = errors.New("ledger: merchant not found")
	// ErrInstallmentNotFound is returned when an installment row is absent.
	ErrInstallmentNotFound = errors.New("ledger: installment not found")
	// ErrInvalidInstallmentSplit is returned by SplitInstallments for inputs
	// that cannot produce a valid schedule.
	ErrInvalidInstallmentSplit = errors.New("ledger: invalid installment split")
	// ErrUnsupportedAccountType is returned when an account carries a type the
	// ledger does not understand.
	ErrUnsupportedAccountType = errors.New("ledger: unsupported account type")
	// ErrInvalidFee is returned when a computed platform fee is invalid, e.g.
	// it consumes the entire principal leaving the merchant a zero payout.
	ErrInvalidFee = errors.New("ledger: invalid platform fee")
	// ErrInstallmentNotPayable is returned when an installment is not in a
	// state that permits payment.
	ErrInstallmentNotPayable = errors.New("ledger: installment is not payable")
	// ErrInvalidOrder is returned when order creation inputs are invalid.
	ErrInvalidOrder = errors.New("ledger: invalid order")
)
