-- name: InsertIdempotencyKey :execresult
-- Pre-flight claim (docs/0002). A duplicate-key error (1062) means another
-- request already claimed this key for this endpoint.
INSERT INTO idempotency_keys (key_value, endpoint, request_hash, status, expires_at)
VALUES (?, ?, ?, 'PROCESSING', ?);

-- name: GetIdempotencyKey :one
SELECT *
FROM idempotency_keys
WHERE key_value = ?
  AND endpoint = ?;

-- name: CompleteIdempotencyKey :exec
UPDATE idempotency_keys
SET status = 'COMPLETED',
    response_status = ?,
    response_body = ?
WHERE key_value = ?
  AND endpoint = ?;

-- name: DeleteExpiredIdempotencyKeys :execrows
DELETE FROM idempotency_keys
WHERE expires_at < ?;
