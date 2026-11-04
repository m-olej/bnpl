package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/molej/bnpl/backend/internal/db/sqlc"
	"github.com/molej/bnpl/backend/internal/platform/database"
)

// Reference types stored on journal_entries.reference_type.
const (
	ReferenceTypeOrder       = "order"
	ReferenceTypeInstallment = "installment"
)

// Repository is the transactional, type-safe data access layer for the
// double-entry ledger. Every mutating method runs inside a single READ
// COMMITTED transaction and takes pessimistic locks in a deterministic order
// (accounts ascending by id) to avoid deadlocks.
type Repository struct {
	q  *sqlc.Queries
	tx *database.TxManager
}

// NewRepository builds a Repository over an open *sql.DB.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		q:  sqlc.New(db),
		tx: database.NewTxManager(db),
	}
}

// PostJournal validates and persists an immutable journal entry together with
// its postings. It locks every touched account with SELECT ... FOR UPDATE,
// inserts the entry and postings, then re-reads the persisted totals to prove
// the double-entry invariant holds at the database level.
//
// Returns the new journal entry id.
func (r *Repository) PostJournal(ctx context.Context, in JournalEntryInput) (int64, error) {
	if err := in.Validate(); err != nil {
		return 0, err
	}

	var journalID int64
	err := r.tx.WithTx(ctx, func(tx *sql.Tx) error {
		id, err := postJournal(ctx, r.q.WithTx(tx), in)
		if err != nil {
			return err
		}
		journalID = id
		return nil
	})
	if err != nil {
		return 0, err
	}
	return journalID, nil
}

// Balance returns the derived balance of an account. Balances are never stored
// as mutable columns; they are computed by summing postings.
func (r *Repository) Balance(ctx context.Context, accountID int64) (Balance, error) {
	row, err := r.q.GetAccountBalance(ctx, uint64(accountID))
	if err != nil {
		return Balance{}, fmt.Errorf("reading balance for account %d: %w", accountID, err)
	}
	return Balance{
		AccountID:   accountID,
		DebitTotal:  row.DebitTotal,
		CreditTotal: row.CreditTotal,
	}, nil
}

// EnsureAccount idempotently creates an account and returns its id.
func (r *Repository) EnsureAccount(
	ctx context.Context,
	code, name string,
	accountType AccountType,
	normalBalance NormalBalance,
	ownerType OwnerType,
	ownerID int64,
	currency string,
) (int64, error) {
	id, err := ensureAccount(ctx, r.q, code, name, accountType, normalBalance, ownerType, ownerID, currency)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// GetAccountByCodeOwner loads a single account by its natural key.
func (r *Repository) GetAccountByCodeOwner(
	ctx context.Context,
	code string,
	ownerType OwnerType,
	ownerID int64,
) (Account, error) {
	row, err := r.q.GetAccountByCodeOwner(ctx, sqlc.GetAccountByCodeOwnerParams{
		Code:      code,
		OwnerType: sqlc.AccountsOwnerType(ownerType),
		OwnerID:   uint64(ownerID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Account{}, fmt.Errorf("%w: code=%s owner=%s/%d", ErrAccountNotFound, code, ownerType, ownerID)
		}
		return Account{}, fmt.Errorf("loading account %s/%s/%d: %w", code, ownerType, ownerID, err)
	}
	return mapAccount(row), nil
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// postJournal is the transaction-scoped core used by both PostJournal and the
// order/installment workflows.
func postJournal(ctx context.Context, q *sqlc.Queries, in JournalEntryInput) (int64, error) {
	if err := in.Validate(); err != nil {
		return 0, err
	}
	if err := lockAccounts(ctx, q, postingAccountIDs(in.Postings)); err != nil {
		return 0, err
	}

	journalID, err := insertJournalEntry(ctx, q, in)
	if err != nil {
		return 0, err
	}
	for i, p := range in.Postings {
		if err := q.InsertPosting(ctx, sqlc.InsertPostingParams{
			JournalEntryID: uint64(journalID),
			AccountID:      uint64(p.AccountID),
			Direction:      sqlc.PostingsDirection(p.Direction),
			Amount:         p.Amount,
			Currency:       p.Currency,
		}); err != nil {
			return 0, fmt.Errorf("inserting posting[%d]: %w", i, err)
		}
	}

	if err := assertJournalBalanced(ctx, q, journalID); err != nil {
		return 0, err
	}
	return journalID, nil
}

func insertJournalEntry(ctx context.Context, q *sqlc.Queries, in JournalEntryInput) (int64, error) {
	occurredAt := in.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	res, err := q.InsertJournalEntry(ctx, sqlc.InsertJournalEntryParams{
		EntryType:      in.EntryType,
		ReferenceType:  in.ReferenceType,
		ReferenceID:    uint64(in.ReferenceID),
		Description:    in.Description,
		TraceID:        in.TraceID,
		IdempotencyKey: nullString(in.IdempotencyKey),
		OccurredAt:     occurredAt,
	})
	if err != nil {
		return 0, fmt.Errorf("inserting journal entry: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("reading journal entry id: %w", err)
	}
	return id, nil
}

func assertJournalBalanced(ctx context.Context, q *sqlc.Queries, journalID int64) error {
	totals, err := q.GetJournalTotals(ctx, uint64(journalID))
	if err != nil {
		return fmt.Errorf("reading persisted journal totals: %w", err)
	}
	if totals.DebitTotal != totals.CreditTotal {
		return fmt.Errorf("%w: journal_id=%d debit=%d credit=%d",
			ErrUnbalancedJournal, journalID, totals.DebitTotal, totals.CreditTotal)
	}
	return nil
}

// lockAccounts takes SELECT ... FOR UPDATE locks on the given accounts in
// ascending id order. Deterministic ordering is what prevents deadlocks when
// two transactions touch the same set of accounts.
func lockAccounts(ctx context.Context, q *sqlc.Queries, ids []int64) error {
	for _, id := range ids {
		if _, err := q.GetAccountByIDForUpdate(ctx, uint64(id)); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: id=%d", ErrAccountNotFound, id)
			}
			return fmt.Errorf("locking account %d: %w", id, err)
		}
	}
	return nil
}

// postingAccountIDs returns the distinct account ids referenced by postings,
// sorted ascending, so locks are always acquired in the same order.
func postingAccountIDs(postings []PostingInput) []int64 {
	seen := make(map[int64]struct{}, len(postings))
	ids := make([]int64, 0, len(postings))
	for _, p := range postings {
		if _, ok := seen[p.AccountID]; ok {
			continue
		}
		seen[p.AccountID] = struct{}{}
		ids = append(ids, p.AccountID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func ensureAccount(
	ctx context.Context,
	q *sqlc.Queries,
	code, name string,
	accountType AccountType,
	normalBalance NormalBalance,
	ownerType OwnerType,
	ownerID int64,
	currency string,
) (int64, error) {
	res, err := q.UpsertAccount(ctx, sqlc.UpsertAccountParams{
		Code:          code,
		Name:          name,
		AccountType:   sqlc.AccountsAccountType(accountType),
		NormalBalance: sqlc.AccountsNormalBalance(normalBalance),
		OwnerType:     sqlc.AccountsOwnerType(ownerType),
		OwnerID:       uint64(ownerID),
		Currency:      currency,
	})
	if err != nil {
		return 0, fmt.Errorf("upserting account %s/%s/%d: %w", code, ownerType, ownerID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("reading account id for %s/%s/%d: %w", code, ownerType, ownerID, err)
	}
	return id, nil
}

func systemAccountID(ctx context.Context, q *sqlc.Queries, code string) (int64, error) {
	acc, err := q.GetAccountByCodeOwner(ctx, sqlc.GetAccountByCodeOwnerParams{
		Code:      code,
		OwnerType: sqlc.AccountsOwnerTypeSYSTEM,
		OwnerID:   0,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("%w: system account %s missing (run migrations)", ErrAccountNotFound, code)
		}
		return 0, fmt.Errorf("loading system account %s: %w", code, err)
	}
	return int64(acc.ID), nil
}

// outstandingReceivable returns the customer's current receivable balance
// (debits minus credits, clamped at zero). A missing receivable account means
// the customer has no outstanding balance yet.
func outstandingReceivable(ctx context.Context, q *sqlc.Queries, customerID int64) (int64, error) {
	acc, err := q.GetAccountByCodeOwner(ctx, sqlc.GetAccountByCodeOwnerParams{
		Code:      CodeCustomerReceivable,
		OwnerType: sqlc.AccountsOwnerTypeCUSTOMER,
		OwnerID:   uint64(customerID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("loading receivable for customer %d: %w", customerID, err)
	}
	bal, err := q.GetAccountBalance(ctx, acc.ID)
	if err != nil {
		return 0, fmt.Errorf("reading receivable balance for customer %d: %w", customerID, err)
	}
	net := bal.DebitTotal - bal.CreditTotal
	if net < 0 {
		return 0, nil
	}
	return net, nil
}

func nullString(s *string) sql.NullString {
	if s == nil || *s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func mapAccount(a sqlc.Account) Account {
	return Account{
		ID:            int64(a.ID),
		Code:          a.Code,
		Name:          a.Name,
		AccountType:   AccountType(a.AccountType),
		NormalBalance: NormalBalance(a.NormalBalance),
		OwnerType:     OwnerType(a.OwnerType),
		OwnerID:       int64(a.OwnerID),
		Currency:      a.Currency,
	}
}
