-- name: InsertOrder :execresult
INSERT INTO orders
    (order_ref, customer_id, merchant_id, principal_amount, currency,
     installment_count, fee_amount, status, idempotency_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetOrderByID :one
SELECT *
FROM orders
WHERE id = ?;

-- name: GetOrderByIDForUpdate :one
SELECT *
FROM orders
WHERE id = ?
FOR UPDATE;

-- name: GetOrderByRef :one
SELECT *
FROM orders
WHERE order_ref = ?;

-- name: GetOrderByRefForUpdate :one
SELECT *
FROM orders
WHERE order_ref = ?
FOR UPDATE;

-- name: GetOrderByIdempotencyKey :one
SELECT *
FROM orders
WHERE idempotency_key = ?;

-- name: UpdateOrderStatus :exec
UPDATE orders
SET status = ?
WHERE id = ?;
