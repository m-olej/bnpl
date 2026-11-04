# BNPL Split-Payment Engine

An idempotent, double-entry financial ledger and multi-service Buy-Now-Pay-Later
checkout platform. Backend in Go + MySQL 8 (via `sqlc`), frontend in React +
TypeScript.

Design rules live in [`AGENTS.md`](./AGENTS.md); the architectural decisions are
recorded in [`docs/`](./docs). The phased build plan is in
[`IMPLEMENTATION_PLAN.md`](./IMPLEMENTATION_PLAN.md), and running notes for each
completed phase are in [`IMPLEMENTATION_PROGRESS.md`](./IMPLEMENTATION_PROGRESS.md).

## Repository layout

```
backend/                     Go module (github.com/molej/bnpl/backend)
  cmd/                       service binaries (checkout, underwriting)
  internal/db/queries/       sqlc query sources (*.sql)
  internal/db/sqlc/          generated, type-safe data access (do not edit)
  internal/ledger/           double-entry domain, repository, scheduling
  internal/platform/         MySQL connection pool + transaction manager
  migrations/                golang-migrate raw SQL (up/down)
  tests/                     integration tests (build tag: integration)
frontend/                    React + TypeScript app (Vite)
docs/                        ADRs
docker-compose.yml           local MySQL 8
Makefile                     developer task shortcuts
```

## Financial invariants

1. **No floating point money.** Every monetary amount is a signed 64-bit
   integer of minor units (`int64` / `BIGINT`). Installment rounding goes
   deterministically to the first installment.
2. **Double-entry, append-only.** Journal entries and postings are immutable
   (enforced by database triggers); corrections are compensating entries. Within
   a transaction `SUM(DEBIT) - SUM(CREDIT) = 0`.
3. **Derived balances.** `accounts` has no mutable balance column; balances are
   computed by summing postings.
4. **Pessimistic locking.** Mutations take `SELECT ... FOR UPDATE` locks in a
   deterministic order; the due-installment worker uses
   `SELECT ... FOR UPDATE SKIP LOCKED`.

## Getting started

```bash
# 1. Start MySQL 8
make compose-up && make compose-wait

# 2. Apply migrations
make migrate-up

# 3. Build, lint and unit-test
make verify

# 4. Integration tests against the running database
cd backend && BNPL_TEST_DSN='bnpl:bnplpw@tcp(127.0.0.1:3306)/bnpl?parseTime=true&loc=UTC' \
  go test -race -tags=integration ./tests/...
```

Copy [`.env.example`](./.env.example) to `.env` to override defaults.

## Tooling

The workflows assume `sqlc`, `golangci-lint` and `migrate` are on `PATH`:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install -tags 'mysql' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```
