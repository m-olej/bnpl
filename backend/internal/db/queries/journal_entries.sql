-- name: InsertJournalEntry :execresult
INSERT INTO journal_entries
    (entry_type, reference_type, reference_id, description, trace_id, idempotency_key, occurred_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetJournalEntryByID :one
SELECT *
FROM journal_entries
WHERE id = ?;

-- name: ListJournalEntriesByReference :many
SELECT *
FROM journal_entries
WHERE reference_type = ?
  AND reference_id = ?
ORDER BY id;
