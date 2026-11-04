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

-- name: CancelInstallment :exec
-- Used when a refund reduces an order's outstanding principal below its
-- scheduled amount; the tail installments are cancelled so the worker cannot
-- collect a refunded portion.
UPDATE installments
SET status = 'CANCELLED'
WHERE id = ?;

-- name: UpdateInstallmentAmount :exec
-- Adjusts the boundary installment when a refund only partially covers it.
UPDATE installments
SET amount = ?
WHERE id = ?;

-- name: RequeueStaleProcessing :execrows
-- Recovery for workers that claimed an installment (marked PROCESSING) and
-- then crashed before finalising it. Anything stuck in PROCESSING older than
-- the cutoff is returned to the SCHEDULED pool for another worker to claim.
UPDATE installments
SET status = 'SCHEDULED'
WHERE status = 'PROCESSING'
  AND updated_at < ?;
