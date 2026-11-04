# Implementation Progress

Running log of completed phases, implementation-specific clarifications, and
open questions for the next phase. Append a new section per completed phase.

---

## Phase 1 — Schema & Ledger Core ✅ (2026-11-04)

### Delivered

- **Local infra:** `docker-compose.yml` running MySQL 8.0 (InnoDB, READ
  COMMITTED, utf8mb4), healthcheck, named volume. `.env.example` documents all
  tunables.
- **Migrations** (`backend/migrations`, golang-migrate up/down pairs):
  - `000001_ledger_core` — `customers`, `merchants`, `accounts`,
    `orders`, `journal_entries`, `postings`, `installments`,
    `idempotency_keys`.
  - `000002_ledger_immutability` — `BEFORE UPDATE`/`BEFORE DELETE` triggers that
    `SIGNAL SQLSTATE '45000'` on `journal_entries` and `postings`.
  - `000003_seed_system_accounts` — shared SYSTEM chart of accounts
    (`CASH_CLEARING`, `MERCHANT_FEE_REVENUE`, `LATE_FEE_REVENUE`,
    `CREDIT_LOSS_EXPENSE`, `PLATFORM_EQUITY`).
- **sqlc** (`backend/sqlc.yaml`, `internal/db/queries/*.sql`,
  generated `internal/db/sqlc`): type-safe queries for accounts, journal
  entries, postings, orders, installments, customers, merchants.
- **Ledger core** (`backend/internal/ledger`):
  - `types.go` — domain enums, `Account`, `PostingInput`, `JournalEntryInput`
    with full invariant validation (min 2 postings, positive magnitudes, single
    currency, debit==credit, int64 overflow guards), derived `Balance`.
  - `repository.go` — transactional `PostJournal` (locks accounts ascending by
    id, inserts entry + postings, re-reads persisted totals to assert balance),
    `Balance`, `EnsureAccount`, `GetAccountByCodeOwner`.
  - `order.go` — `CreateOrderWithSchedule` (atomic: lock parties → fee →
    sub-ledger accounts → insert order → `ORDER_AUTHORIZED` entry → schedule).
  - `installment.go` — `PayInstallment` (locks installment + order, posts
    `INSTALLMENT_PAID`, advances order to ACTIVE/COMPLETED) and
    `ClaimDueInstallments` (`FOR UPDATE SKIP LOCKED`, marks PROCESSING, commits
    before gateway work).
  - `party.go` — `CreateCustomer`, `CreateMerchant`,
    `GetCustomerByExternalRef`.
  - `schedule.go` / `fee.go` — deterministic installment splitting and
    basis-point fee math.
- **Platform** (`internal/platform/database`) — connection pool + `WithTx`
  transaction manager (READ COMMITTED, panic-safe rollback).
- **Tests:**
  - Unit (table-driven, `-race`): `schedule_test.go`, `types_test.go`,
    `fee_test.go` — zero/negative/overflow boundaries, canonical
    `$100.00 / 3 → 3334, 3333, 3333`, fee flooring, mixed currency, unknown
    direction.
  - Integration (`tests/`, build tag `integration`) — full order → schedule →
    repayment lifecycle, derived balances, and DB trigger immutability.
- **Tooling/docs:** `Makefile`, `backend/.golangci.yml`, updated `README.md`,
  updated `AGENTS.md` workflow paths (now `backend/` and `frontend/`).

### Verification (all green)

| Command | Result |
| --- | --- |
| `go build ./...` (backend) | OK |
| `go vet ./...` (backend) | OK |
| `golangci-lint run ./...` (backend) | 0 issues |
| `go test -race ./internal/...` | ok |
| `migrate up` against MySQL 8.0 | 1,2,3 applied |
| `go test -race -tags=integration ./tests/...` | ok |

### Implementation-specific clarifications (carry into Phase 2)

1. **Migration/trigger privilege.** MySQL 8 has binary logging on by default, so
   creating the immutability triggers fails with `Error 1419` unless the server
   sets `log_bin_trust_function_creators=1`. `docker-compose.yml` now passes
   `--log-bin-trust-function-creators=1`. **Phase 4 testcontainers must set the
   same flag** (e.g. `testcontainers.WithCmd("--log-bin-trust-function-creators=1")`).
2. **`multiStatements=true`.** golang-migrate needs it in the DSN to run the
   multi-statement migration files. It is present in `.env.example`, the
   `Makefile` default DSN, and the integration-test DSN. Note it must **not** be
   enabled on application connection pools used for request handling (SQL
   injection surface); the app DSN in `.env.example` currently includes it only
   to keep one DSN — **Phase 2 should split `DB_DSN` (app, no
   `multiStatements`) from `MIGRATE_DSN` (migrations, with it).**
3. **sqlc schema scope.** `sqlc.yaml` points `schema` at only
   `000001_ledger_core.up.sql`. Seed data (000003) and triggers (000002) are
   deliberately excluded — sqlc's parser should not see DML/triggers. When a
   future migration changes columns, add its `.up.sql` to the `schema` list and
   run `make sqlc`.
4. **Generated types.** MySQL `ENUM`s become named string types
   (`sqlc.AccountsOwnerType`, …) and ids are `uint64`. The `ledger` package
   exposes `int64` everywhere (money *and* ids) and converts at the boundary.
5. **Balance sign convention.** `Balance.Net()` is raw `debit-credit`;
   `Balance.Signed(normalBalance)` adjusts for the account's normal side.
6. **Rounding policy (see open question Q1).** `SplitInstallments` puts the
   integer-division remainder on the **first** installment, matching the literal
   AGENTS.md rule. Fee `CalculateFee` **floors** (`principal*feeBps/10000`).
7. **Account scoping.** `accounts` is uniquely keyed by
   `(code, owner_type, owner_id)`. SYSTEM accounts use `owner_id=0`; per-party
   sub-ledgers (`CUSTOMER_RECEIVABLE`, `MERCHANT_PAYABLE`) are upserted on
   demand inside the order transaction.
8. **Lock ordering.** Party rows are locked customer→merchant; account locks are
   always acquired ascending by id (`postingAccountIDs` sorts). Installment work
   locks installment → order → accounts.
9. **Idempotency schema exists** (`idempotency_keys`, ADR-002) but no middleware
   yet — that is Phase 2. `orders.idempotency_key` is a unique column already.
10. **`go.mod` module path** is `github.com/molej/bnpl/backend`. `google/uuid`
    was dropped by `go mod tidy` (unused in Phase 1).

### Open questions for Phase 2 (please confirm)

- **Q1 (rounding):** For a multi-cent remainder (e.g. `$100.01 / 3`), the
  current rule puts **all** remainder cents on installment #1
  (`3335, 3333, 3333`). The common alternative is to spread remainder cents one
  per leading installment (`3334, 3334, 3333`). Which is authoritative?
- **Q2 (credit limits):** `customers.credit_limit` exists and is locked, but
  Phase 1 does **not** enforce it. Should the Underwriting Service reserve
  available credit at authorization (limit − outstanding receivables), and where
  is that check enforced — service layer only, or also a repository guard?
- **Q3 (merchant payout timing):** Phase 1 recognizes merchant payable at
  authorization (platform assumes credit risk; merchant paid up front,
  settlement via a later `MERCHANT_PAYOUT` entry). Confirm this is the intended
  model vs. paying the merchant only as installments are collected.
- **Q4 (API/auth scope):** No auth model is specified. For the Phase 2 REST
  APIs, should I add a placeholder merchant/customer identity (e.g. header-based)
  or leave endpoints unauthenticated for the demo?
- **Q5 (currency):** The ledger validates single-currency entries but does not
  model FX. Assume single-currency (`USD`) per deployment for now?
- **Q6 (order references):** Should the Checkout Service generate `order_ref`
  (UUID) server-side, or accept a merchant-supplied reference?

### Phase 2 preview

Build `cmd/checkout` and `cmd/underwriting` services with: `Idempotency-Key`
middleware (ADR-002), structured `slog` JSON logging (`trace_id`, `user_id`,
`idempotency_key`), OpenAPI/Swagger specs, and REST handlers over this ledger
repository. Split the app DSN from the migration DSN (see clarification 2).
