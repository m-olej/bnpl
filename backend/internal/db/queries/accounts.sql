-- name: InsertAccount :execresult
INSERT INTO accounts (code, name, account_type, normal_balance, owner_type, owner_id, currency)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpsertAccount :execresult
-- Idempotent account creation. LAST_INSERT_ID(id) makes LastInsertId() return
-- the pre-existing row's id when the unique (code, owner_type, owner_id) key
-- already exists, which lets the repository avoid a read-then-write race.
INSERT INTO accounts (code, name, account_type, normal_balance, owner_type, owner_id, currency)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id);

-- name: GetAccountByID :one
SELECT *
FROM accounts
WHERE id = ?;

-- name: GetAccountByIDForUpdate :one
-- Pessimistic lock used before any mutation that must be serialized.
SELECT *
FROM accounts
WHERE id = ?
FOR UPDATE;

-- name: GetAccountByCodeOwner :one
SELECT *
FROM accounts
WHERE code = ?
  AND owner_type = ?
  AND owner_id = ?;
