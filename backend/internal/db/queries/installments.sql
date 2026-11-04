-- name: InsertInstallment :execresult
INSERT INTO installments
    (order_id, customer_id, installment_no, amount, currency, due_date, status)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListInstallmentsByOrder :many
SELECT *
FROM installments
WHERE order_id = ?
ORDER BY installment_no;

-- name: GetInstallmentByID :one
SELECT *
FROM installments
WHERE id = ?;

-- name: GetInstallmentByIDForUpdate :one
SELECT *
FROM installments
WHERE id = ?
FOR UPDATE;

-- name: ClaimDueInstallments :many
-- Distributed worker claim (docs/0003). SKIP LOCKED lets horizontally scaled
-- workers drain the due queue without blocking on each other.
SELECT *
FROM installments
WHERE status = 'SCHEDULED'
  AND due_date <= ?
ORDER BY due_date ASC
LIMIT ?
FOR UPDATE SKIP LOCKED;

-- name: MarkInstallmentProcessing :exec
UPDATE installments
SET status = 'PROCESSING',
    attempt_count = attempt_count + 1
WHERE id = ?;

-- name: MarkInstallmentPaid :exec
UPDATE installments
SET status = 'PAID',
    paid_at = ?,
    journal_entry_id = ?,
    last_error = NULL
WHERE id = ?;

-- name: MarkInstallmentFailed :exec
UPDATE installments
SET status = 'FAILED',
    last_error = ?
WHERE id = ?;

-- name: RescheduleInstallment :exec
UPDATE installments
SET status = 'SCHEDULED'
WHERE id = ?;
