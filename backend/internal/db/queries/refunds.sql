-- name: SumCollectedInstallments :one
-- Total already collected for an order.
SELECT CAST(COALESCE(SUM(amount), 0) AS SIGNED) AS collected
FROM installments
WHERE order_id = ?
  AND status = 'PAID';

-- name: SumOrderRefundPostings :one
-- Aggregates prior REFUND entries for an order: principal credited back to the
-- customer receivable, and fee debited from fee revenue (for proportional
-- reversal).
SELECT
    CAST(COALESCE(SUM(CASE WHEN p.account_id = sqlc.arg('receivable_account_id') AND p.direction = 'CREDIT' THEN p.amount ELSE 0 END), 0) AS SIGNED) AS refunded_principal,
    CAST(COALESCE(SUM(CASE WHEN p.account_id = sqlc.arg('fee_account_id') AND p.direction = 'DEBIT' THEN p.amount ELSE 0 END), 0) AS SIGNED) AS refunded_fee
FROM journal_entries je
JOIN postings p ON p.journal_entry_id = je.id
WHERE je.reference_type = 'order'
  AND je.reference_id = sqlc.arg('order_id')
  AND je.entry_type = 'REFUND';
