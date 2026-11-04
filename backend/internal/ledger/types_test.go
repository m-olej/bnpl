package ledger

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestJournalEntryValidate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	posting := func(account int64, dir Direction, amount int64, currency string) PostingInput {
		return PostingInput{AccountID: account, Direction: dir, Amount: amount, Currency: currency}
	}

	tests := []struct {
		name    string
		entry   JournalEntryInput
		wantErr error
	}{
		{
			name: "balanced authorization",
			entry: JournalEntryInput{
				EntryType: EntryOrderAuthorized, ReferenceType: "order", ReferenceID: 1, OccurredAt: now,
				Postings: []PostingInput{
					posting(1, Debit, 10000, "USD"),
					posting(2, Credit, 9500, "USD"),
					posting(3, Credit, 500, "USD"),
				},
			},
		},
		{
			name: "unbalanced rejects",
			entry: JournalEntryInput{
				OccurredAt: now,
				Postings: []PostingInput{
					posting(1, Debit, 10000, "USD"),
					posting(2, Credit, 9999, "USD"),
				},
			},
			wantErr: ErrUnbalancedJournal,
		},
		{
			name: "single posting rejects",
			entry: JournalEntryInput{
				OccurredAt: now,
				Postings:   []PostingInput{posting(1, Debit, 100, "USD")},
			},
			wantErr: ErrTooFewPostings,
		},
		{
			name:    "no postings rejects",
			entry:   JournalEntryInput{OccurredAt: now},
			wantErr: ErrTooFewPostings,
		},
		{
			name: "zero amount rejects",
			entry: JournalEntryInput{
				OccurredAt: now,
				Postings: []PostingInput{
					posting(1, Debit, 0, "USD"),
					posting(2, Credit, 0, "USD"),
				},
			},
			wantErr: ErrNonPositiveAmount,
		},
		{
			name: "negative amount rejects",
			entry: JournalEntryInput{
				OccurredAt: now,
				Postings: []PostingInput{
					posting(1, Debit, -5, "USD"),
					posting(2, Credit, 5, "USD"),
				},
			},
			wantErr: ErrNonPositiveAmount,
		},
		{
			name: "unknown direction rejects",
			entry: JournalEntryInput{
				OccurredAt: now,
				Postings: []PostingInput{
					posting(1, Direction("SIDEWAYS"), 100, "USD"),
					posting(2, Credit, 100, "USD"),
				},
			},
			wantErr: ErrUnknownDirection,
		},
		{
			name: "mixed currency rejects",
			entry: JournalEntryInput{
				OccurredAt: now,
				Postings: []PostingInput{
					posting(1, Debit, 100, "USD"),
					posting(2, Credit, 100, "EUR"),
				},
			},
			wantErr: ErrMixedCurrency,
		},
		{
			name: "debit overflow rejects",
			entry: JournalEntryInput{
				OccurredAt: now,
				Postings: []PostingInput{
					posting(1, Debit, math.MaxInt64, "USD"),
					posting(2, Debit, 1, "USD"),
					posting(3, Credit, 1, "USD"),
				},
			},
			wantErr: ErrAmountOverflow,
		},
		{
			name: "credit overflow rejects",
			entry: JournalEntryInput{
				OccurredAt: now,
				Postings: []PostingInput{
					posting(1, Credit, math.MaxInt64, "USD"),
					posting(2, Credit, 1, "USD"),
					posting(3, Debit, 1, "USD"),
				},
			},
			wantErr: ErrAmountOverflow,
		},
		{
			name: "max int64 balanced accepted",
			entry: JournalEntryInput{
				OccurredAt: now,
				Postings: []PostingInput{
					posting(1, Debit, math.MaxInt64, "USD"),
					posting(2, Credit, math.MaxInt64, "USD"),
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.entry.Validate()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestBalanceSigned(t *testing.T) {
	t.Parallel()

	// Debit-normal asset account increased: debit 300, credit 100.
	asset := Balance{AccountID: 1, DebitTotal: 300, CreditTotal: 100}
	if got := asset.Net(); got != 200 {
		t.Errorf("Net() = %d, want 200", got)
	}
	if got := asset.Signed(NormalDebit); got != 200 {
		t.Errorf("Signed(DEBIT) = %d, want 200", got)
	}
	if got := asset.Signed(NormalCredit); got != -200 {
		t.Errorf("Signed(CREDIT) = %d, want -200", got)
	}

	// Credit-normal liability increased: credit 500, debit 50.
	liability := Balance{AccountID: 2, DebitTotal: 50, CreditTotal: 500}
	if got := liability.Signed(NormalCredit); got != 450 {
		t.Errorf("Signed(CREDIT) = %d, want 450", got)
	}
}
