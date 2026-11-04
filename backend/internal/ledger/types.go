package ledger

import (
	"fmt"
	"math"
	"time"
)

// AccountType classifies an account within the chart of accounts.
type AccountType string

// Supported account types.
const (
	AccountTypeAsset     AccountType = "ASSET"
	AccountTypeLiability AccountType = "LIABILITY"
	AccountTypeEquity    AccountType = "EQUITY"
	AccountTypeRevenue   AccountType = "REVENUE"
	AccountTypeExpense   AccountType = "EXPENSE"
)

// NormalBalance is the side on which an account increases.
type NormalBalance string

// Normal balances.
const (
	NormalDebit  NormalBalance = "DEBIT"
	NormalCredit NormalBalance = "CREDIT"
)

// OwnerType scopes an account to the system or a specific party.
type OwnerType string

// Account owner types.
const (
	OwnerSystem   OwnerType = "SYSTEM"
	OwnerCustomer OwnerType = "CUSTOMER"
	OwnerMerchant OwnerType = "MERCHANT"
)

// Direction is the side of a posting.
type Direction string

// Posting directions.
const (
	Debit  Direction = "DEBIT"
	Credit Direction = "CREDIT"
)

// Well-known account codes. Per-party accounts are scoped by owner_type and
// owner_id; system accounts use owner_id 0.
const (
	CodeCashClearing       = "CASH_CLEARING"
	CodeCustomerReceivable = "CUSTOMER_RECEIVABLE"
	CodeMerchantPayable    = "MERCHANT_PAYABLE"
	CodeMerchantFeeRevenue = "MERCHANT_FEE_REVENUE"
	CodeLateFeeRevenue     = "LATE_FEE_REVENUE"
	CodeCreditLossExpense  = "CREDIT_LOSS_EXPENSE"
	CodePlatformEquity     = "PLATFORM_EQUITY"
)

// Journal entry types.
const (
	EntryOrderAuthorized = "ORDER_AUTHORIZED"
	EntryInstallmentPaid = "INSTALLMENT_PAID"
	EntryMerchantPayout  = "MERCHANT_PAYOUT"
	EntryRefund          = "REFUND"
	EntryLateFee         = "LATE_FEE"
	EntryCreditWriteOff  = "CREDIT_WRITE_OFF"
)

// PostingInput is a single debit or credit to apply to an account.
type PostingInput struct {
	AccountID int64
	Direction Direction
	// Amount is a positive magnitude in minor units (cents).
	Amount   int64
	Currency string
}

// JournalEntryInput describes an immutable journal entry and its postings.
type JournalEntryInput struct {
	EntryType      string
	ReferenceType  string
	ReferenceID    int64
	Description    string
	TraceID        string
	IdempotencyKey *string
	OccurredAt     time.Time
	Postings       []PostingInput
}

// Validate checks every structural and double-entry invariant that can be
// evaluated without touching the database.
func (j JournalEntryInput) Validate() error {
	if len(j.Postings) < 2 {
		return fmt.Errorf("%w: got %d", ErrTooFewPostings, len(j.Postings))
	}

	var debit, credit int64
	currency := ""
	for i, p := range j.Postings {
		if p.Amount <= 0 {
			return fmt.Errorf("%w: posting[%d] amount=%d", ErrNonPositiveAmount, i, p.Amount)
		}
		switch p.Direction {
		case Debit:
			sum, err := safeAdd(debit, p.Amount)
			if err != nil {
				return fmt.Errorf("validating debits: %w", err)
			}
			debit = sum
		case Credit:
			sum, err := safeAdd(credit, p.Amount)
			if err != nil {
				return fmt.Errorf("validating credits: %w", err)
			}
			credit = sum
		default:
			return fmt.Errorf("%w: posting[%d] direction=%q", ErrUnknownDirection, i, p.Direction)
		}

		switch {
		case currency == "":
			currency = p.Currency
		case p.Currency != currency:
			return fmt.Errorf("%w: %q != %q", ErrMixedCurrency, p.Currency, currency)
		}
	}

	if debit != credit {
		return fmt.Errorf("%w: debit=%d credit=%d", ErrUnbalancedJournal, debit, credit)
	}
	return nil
}

// Totals returns the summed debit and credit magnitudes.
func (j JournalEntryInput) Totals() (debit, credit int64, err error) {
	for i, p := range j.Postings {
		if p.Amount <= 0 {
			return 0, 0, fmt.Errorf("%w: posting[%d] amount=%d", ErrNonPositiveAmount, i, p.Amount)
		}
		switch p.Direction {
		case Debit:
			if debit, err = safeAdd(debit, p.Amount); err != nil {
				return 0, 0, err
			}
		case Credit:
			if credit, err = safeAdd(credit, p.Amount); err != nil {
				return 0, 0, err
			}
		default:
			return 0, 0, fmt.Errorf("%w: posting[%d] direction=%q", ErrUnknownDirection, i, p.Direction)
		}
	}
	return debit, credit, nil
}

// Account is a chart-of-accounts entry. It carries no mutable balance.
type Account struct {
	ID            int64
	Code          string
	Name          string
	AccountType   AccountType
	NormalBalance NormalBalance
	OwnerType     OwnerType
	OwnerID       int64
	Currency      string
}

// Balance is a derived account balance projection.
type Balance struct {
	AccountID   int64
	DebitTotal  int64
	CreditTotal int64
}

// Net returns debit - credit, the raw signed balance of the account.
// The accounting-sign interpretation depends on the account's normal_balance.
func (b Balance) Net() int64 {
	return b.DebitTotal - b.CreditTotal
}

// Signed returns the balance adjusted for the account's normal side, so a
// positive result always means "increased in the expected direction".
func (b Balance) Signed(normal NormalBalance) int64 {
	switch normal {
	case NormalCredit:
		return b.CreditTotal - b.DebitTotal
	default:
		return b.DebitTotal - b.CreditTotal
	}
}

func safeAdd(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, fmt.Errorf("%w: %d + %d", ErrAmountOverflow, a, b)
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, fmt.Errorf("%w: %d + %d", ErrAmountOverflow, a, b)
	}
	return a + b, nil
}
