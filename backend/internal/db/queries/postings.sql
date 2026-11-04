-- name: InsertPosting :exec
INSERT INTO postings (journal_entry_id, account_id, direction, amount, currency)
VALUES (?, ?, ?, ?, ?);

-- name: ListPostingsByJournal :many
SELECT *
FROM postings
WHERE journal_entry_id = ?
ORDER BY id;

-- name: GetAccountBalance :one
-- Derived projection: balance = sum(debits) - sum(credits). CAST AS SIGNED
-- forces an integer result (MySQL otherwise widens SUM to DECIMAL).
SELECT
    CAST(COALESCE(SUM(CASE WHEN direction = 'DEBIT'  THEN amount ELSE 0 END), 0) AS SIGNED) AS debit_total,
    CAST(COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount ELSE 0 END), 0) AS SIGNED) AS credit_total
FROM postings
WHERE account_id = ?;

-- name: GetJournalTotals :one
-- Used to re-verify the double-entry invariant after insertion.
SELECT
    CAST(COALESCE(SUM(CASE WHEN direction = 'DEBIT'  THEN amount ELSE 0 END), 0) AS SIGNED) AS debit_total,
    CAST(COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount ELSE 0 END), 0) AS SIGNED) AS credit_total
FROM postings
WHERE journal_entry_id = ?;
