-- name: InsertCustomer :execresult
INSERT INTO customers (external_ref, full_name, credit_limit, currency)
VALUES (?, ?, ?, ?);

-- name: GetCustomerByID :one
SELECT *
FROM customers
WHERE id = ?;

-- name: GetCustomerByIDForUpdate :one
-- Must be held before modifying credit_limit (AGENTS.md section 1.3).
SELECT *
FROM customers
WHERE id = ?
FOR UPDATE;

-- name: GetCustomerByExternalRef :one
SELECT *
FROM customers
WHERE external_ref = ?;

-- name: UpdateCustomerCreditLimit :exec
UPDATE customers
SET credit_limit = ?
WHERE id = ?;
