-- name: SumOrderPayoutPostings :one
-- Total already settled to a merchant for an order: the sum of MERCHANT_PAYOUT
-- debits against the merchant payable (used to compute the settlement delta, so
-- settlement stays idempotent across worker retries).
SELECT CAST(COALESCE(SUM(p.amount), 0) AS SIGNED) AS paid_out
FROM journal_entries je
JOIN postings p ON p.journal_entry_id = je.id
WHERE je.reference_type = 'order'
  AND je.reference_id = sqlc.arg('order_id')
  AND je.entry_type = 'MERCHANT_PAYOUT'
  AND p.account_id = sqlc.arg('payable_account_id')
  AND p.direction = 'DEBIT';
