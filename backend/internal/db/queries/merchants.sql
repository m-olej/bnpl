-- name: InsertMerchant :execresult
INSERT INTO merchants (merchant_ref, name, fee_bps, currency)
VALUES (?, ?, ?, ?);

-- name: GetMerchantByID :one
SELECT *
FROM merchants
WHERE id = ?;

-- name: GetMerchantByIDForUpdate :one
SELECT *
FROM merchants
WHERE id = ?
FOR UPDATE;

-- name: GetMerchantByRef :one
SELECT *
FROM merchants
WHERE merchant_ref = ?;
