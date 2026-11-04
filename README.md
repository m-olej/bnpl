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
  cmd/                       service binaries (checkout, underwriting, worker)
  internal/db/queries/       sqlc query sources (*.sql)
  internal/db/sqlc/          generated, type-safe data access (do not edit)
  internal/ledger/           double-entry domain, repository, scheduling
  internal/worker/           due-installment settlement loop (docs/0003)
  internal/platform/         MySQL connection pool + transaction manager
  migrations/                golang-migrate raw SQL (up/down)
  tests/                     integration tests (build tag: integration)
frontend/                    React + TypeScript app (Vite)
  e2e/                       Playwright end-to-end checkout test
docs/                        ADRs
.github/workflows/ci.yml     CI: lint, unit, integration and E2E jobs
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

Copy [`.env.example`](./.env.example) to `.env` to override defaults. Note the
two DSNs: `DB_DSN` for services (no `multiStatements`) and `MIGRATE_DSN` for
migrations (`multiStatements=true`).

If `BNPL_TEST_DSN` is unset, the integration suite starts a throwaway MySQL 8
container via testcontainers (Docker required) and applies the migrations itself.

### Running the services

```bash
make run-underwriting   # :8082
make run-checkout       # :8081
make run-worker         # background installment settlement (docs/0003)
```

Both HTTP services expose `GET /openapi.yaml` and `GET /docs` (Swagger UI). API
routes require placeholder identity headers (`X-Actor-Type`, `X-Actor-Id`);
mutating routes also require an `Idempotency-Key`.

The worker polls for due installments (`SELECT ... FOR UPDATE SKIP LOCKED`),
captures them through a (simulated) payment gateway, records the collection, and
settles the merchant's payable for the collected portion. It is safe to run
multiple replicas: claims are disjoint and settlement is delta-based, so retries
cannot double-pay. Tune it with `WORKER_BATCH_SIZE`, `WORKER_POLL_INTERVAL` and
`WORKER_STALE_AFTER` (see [`.env.example`](./.env.example)).

```bash
curl -s localhost:8082/api/v1/customers \
  -H 'X-Actor-Type: ADMIN' -H 'X-Actor-Id: demo' -H 'Idempotency-Key: c-1' \
  -H 'Content-Type: application/json' \
  -d '{"external_ref":"cust-1","full_name":"Jane Doe","credit_limit":100000}'
```

## Testing

- **Unit tests** (table-driven, `-race`): `make test`.
- **Integration tests** (build tag `integration`, real MySQL 8): see step 4
  above. They cover the concurrency guarantees — the credit-limit row lock,
  `FOR UPDATE SKIP LOCKED` claim disjointness, concurrent refunds, and idempotent
  merchant settlement — plus a full worker collection/settlement cycle.
- **End-to-end** (Playwright): `make frontend-e2e` drives the real React UI
  against both Go services and the database through one checkout → schedule →
  partial refund → full refund flow.

CI (`.github/workflows/ci.yml`) runs three jobs: **backend** (build, vet, lint,
unit and integration tests against a `mysql:8.0` service container),
**frontend** (lint, typecheck, build) and **e2e** (apply migrations, then run
Playwright).

## Tooling

The workflows assume `sqlc`, `golangci-lint` and `migrate` are on `PATH`:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install -tags 'mysql' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```
