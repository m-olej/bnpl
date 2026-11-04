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
6. **Rounding policy (confirmed post-review).** `SplitInstallments` spreads the
   integer-division remainder one minor unit per leading installment
   (largest-remainder), so no two installments differ by more than one minor
   unit: `$100.00 / 3 → 3334, 3333, 3333` and `$100.01 / 3 → 3334, 3334, 3333`.
   Fee `CalculateFee` **floors** (`principal*feeBps/10000`).
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

### Phase 2 decisions (confirmed post-review)

- **Q1 (rounding) — RESOLVED.** Spread remainder one minor unit per leading
  installment. Implemented in this phase; tests and docs updated.
- **Q2 (credit limits) — RESOLVED.** Enforce at authorization in the Underwriting
  Service **and** with a repository guard that reserves available credit
  (limit − outstanding receivables) inside the order transaction, returning
  `ErrInsufficientCredit`.
- **Q4 (auth) — RESOLVED.** Header-based placeholder merchant/customer identity
  validated by middleware (no external auth provider).
- **Q6 (order reference) — RESOLVED.** Checkout Service generates a server-side
  UUID `order_ref` and returns it to the client.
- **Q5 (currency) — PROVISIONAL.** Single currency (`USD`) per deployment; the
  ledger validates single-currency entries but models no FX.

### Open question still needing a concrete decision

- **Q3 (merchant payout timing) — answered "as installments are collected", but
  the ledger mechanics need confirmation.** Two candidate models:

  | Event | Model A: accrue at authorization (settle progressively) | Model B: recognize progressively |
  | --- | --- | --- |
  | `ORDER_AUTHORIZED` | Dr CUSTOMER_RECEIVABLE `P`; Cr MERCHANT_PAYABLE `P−fee`; Cr MERCHANT_FEE_REVENUE `fee` | Dr CUSTOMER_RECEIVABLE `P`; Cr DEFERRED_MERCHANT_PAYABLE `P−fee`; Cr MERCHANT_FEE_REVENUE `fee` |
  | each `INSTALLMENT_PAID` | Dr CASH_CLEARING `amt`; Cr CUSTOMER_RECEIVABLE `amt` | Dr CASH_CLEARING `amt`; Cr CUSTOMER_RECEIVABLE `amt`; **and** move `amt` from DEFERRED to MERCHANT_PAYABLE |
  | `MERCHANT_PAYOUT` | Dr MERCHANT_PAYABLE; Cr CASH_CLEARING | Dr MERCHANT_PAYABLE; Cr CASH_CLEARING |

  Model A keeps the Phase 1 `ORDER_AUTHORIZED` posting unchanged and only defers
  the payout. Model B requires an extra `DEFERRED_MERCHANT_PAYABLE` account and
  per-installment transfers. **Recommendation: Model A** (the obligation is
  recognized when the order is authorized; cash settlement happens as
  collections arrive), unless "progressive recognition" is specifically wanted.
  Awaiting confirmation before Phase 2.

### Phase 2 preview

Build `cmd/checkout` and `cmd/underwriting` services with: `Idempotency-Key`
middleware (ADR-002), structured `slog` JSON logging (`trace_id`, `user_id`,
`idempotency_key`), OpenAPI/Swagger specs, and REST handlers over this ledger
repository. Split the app DSN from the migration DSN (see clarification 2).

---

## Phase 2 — Microservices & Idempotent REST APIs ✅ (2026-11-04)

### Delivered

- **Shared HTTP platform** (`internal/platform/`):
  - `httpkit` — `RequestID`, `Logging` (one structured `slog` line per request
    with `trace_id`, `user_id`, `actor_type`, `idempotency_key`, status,
    duration), `Recoverer`, placeholder header `Auth`, JSON helpers, strict body
    decoding, and domain→HTTP error mapping.
  - `logging` — JSON `slog` handler with `LOG_LEVEL`.
  - `server` — env helpers and graceful-shutdown `Run`.
- **Idempotency** (`internal/idempotency`, ADR-002): `MySQLStore` implementing
  the pre-flight claim protocol plus a `claim → replay / 409 in-flight / 422
  mismatch` middleware, response capture for replay, 24h TTL, and a
  `PurgeExpired` maintenance query.
- **Underwriting service** (`internal/underwriting`, `cmd/underwriting`, :8082):
  onboard customers/merchants, get customer/merchant, `PATCH` credit limit
  (locked), `GET` credit position, and advisory `GET /underwriting/eligibility`.
- **Checkout service** (`internal/checkout`, `cmd/checkout`, :8081):
  `POST /checkout/authorize` (server-generated `ord_<uuid>` reference),
  `GET /orders/{orderRef}`, `GET /orders/{orderRef}/schedule`.
- **Ledger additions**: repository credit guard (`ErrInsufficientCredit`)
  enforced inside the order transaction under the locked customer row;
  `AvailableCredit`, `SetCustomerCreditLimit`, `GetCustomer`, `GetMerchant`,
  `GetMerchantByRef`, and `outstandingReceivable`.
- **OpenAPI 3.1 spec** (`api/openapi.yaml`, embedded) served at `/openapi.yaml`
  with a Swagger UI page at `/docs` on both services.
- **DSN split**: `DB_DSN` (services, no `multiStatements`) vs `MIGRATE_DSN`
  (golang-migrate only) — resolves Phase 1 clarification 2.
- **Tests**: table-driven idempotency middleware unit tests (acquired / replay /
  in-flight / mismatch / missing key / read passthrough / claim error), and HTTP
  end-to-end integration tests (onboard → authorize → idempotent replay →
  payload-mismatch 422 → missing-key 400 → credit position → eligibility →
  402 on insufficient credit).

### Verification (all green)

| Command | Result |
| --- | --- |
| `go build ./...` | OK |
| `go vet -tags=integration ./...` | OK |
| `golangci-lint run ./...` | 0 issues |
| `go test -race ./internal/...` | ok |
| `go test -race -tags=integration ./tests/...` | 5/5 pass |
| `make migrate-up` (split DSN) | no change |
| `cmd/checkout` binary smoke test | `/openapi.yaml` 200, `/docs` 200, unauth API 401, auth 404 |

### Implementation-specific clarifications (carry into Phase 3)

1. **Middleware order** is `RequestID → Logging → Auth → Idempotency →
   Recoverer → handler`. `Recoverer` is innermost so a handler panic is turned
   into a 500 that the idempotency layer *can* cache; the key never gets stuck in
   `PROCESSING`.
2. **Idempotency caches every completed response, including 5xx** (literal
   ADR-002 behaviour). Endpoint identity is `"METHOD /path"`, so the same key on
   a different path is treated as a separate operation. `orders.idempotency_key`
   also carries a unique constraint as a second line of defence.
3. **Completion is a separate write** from the domain transaction. ADR-002
   permits (but does not require) folding it into the domain commit; we kept it
   separate for simplicity. Revisit if exactly-once caching under crash-in-between
   becomes a requirement.
4. **Credit enforcement** is authoritative inside
   `CreateOrderWithSchedule` (customer row locked ⇒ serialized). The underwriting
   `eligibility` endpoint is advisory and read-only.
5. **Auth is a placeholder**: `X-Actor-Type` ∈ {CUSTOMER, MERCHANT, ADMIN,
   SYSTEM} + `X-Actor-Id`. It guards everything under `/api/`; `/docs` and
   `/openapi.yaml` stay public. Replace with real token verification later.
6. **Installment collection and merchant payout are not yet exposed.** The
   ledger methods `PayInstallment` / `ClaimDueInstallments` exist and are tested,
   but there is no HTTP route or worker for them yet, and no `MERCHANT_PAYOUT`
   entry is produced. Per the confirmed **Model A**, merchant payable is
   recognized at authorization; settlement is a future worker/Phase 4 concern.

### Open items for Phase 3

- **Refunds**: implement `RefundOrder` in the ledger (compensating `REFUND`
  entry, fee reversal proportional to the refunded amount) and the
  `POST /orders/{ref}/refunds` endpoint. Deferred from Phase 2 deliberately.
- **Merchant payout / collection worker**: wire `ClaimDueInstallments` +
  `PayInstallment` into a background worker and emit `MERCHANT_PAYOUT`.
- **React frontend** (Phase 3): checkout flow, installment schedule view, and
  partial-refund UI via TanStack Query.

### Phase 3 preview

Build the React + TypeScript (Vite) app in `frontend/` with TanStack Query to
trigger checkouts, inspect schedules, and issue partial refunds, backed by the
new refund endpoint.

---

## Phase 3 — Refunds & React Frontend ✅ (2026-11-04)

### Delivered

- **Refund ledger core** (`backend/internal/ledger/refund.go`):
  - `RefundParams` / `RefundResult` and `RefundOrder(ctx, params)` implementing
    the compensating `REFUND` journal entry: **Credit `CUSTOMER_RECEIVABLE`
    `amount`; Debit `MERCHANT_PAYABLE` `merchantReduction`; Debit
    `MERCHANT_FEE_REVENUE` `feeRefund`**.
  - The order row is locked first (`GetOrderByRefForUpdate`, new sqlc query),
    then accounts are locked ascending by id inside `postJournal` — same lock
    ordering as the rest of the ledger.
  - Outstanding principal is derived, never stored:
    `principal − collected(PAID installments) − prior refund postings`
    (`SumCollectedInstallments`, `SumOrderRefundPostings` in
    `queries/refunds.sql`). Only the outstanding/uncollected principal is
    refundable; there is no cash-disbursement path for already-collected money.
  - Fee reversal is proportional to the outstanding principal
    (`mulDiv`), reversing the **exact remaining fee** on a full refund, and is
    clamped to `[0, amount]` so the balancing merchant-payable debit can never go
    negative (guards heavily-collected orders). Pure helper `refundFee` is
    table-tested.
  - A refund that clears the outstanding principal marks the order `REFUNDED`;
    otherwise the status is left as-is (supporting multiple partial refunds).
  - New sentinels `ErrInvalidRefund` (bad input) and
    `ErrRefundExceedsOutstanding` (`amount > outstanding`).
- **Checkout API**: `Service.Refund`, `POST /api/v1/orders/{orderRef}/refunds`
  (idempotent, body `{amount, reason}`, `201` with fee/merchant splits and
  remaining outstanding), wired through the existing middleware chain.
- **Error mapping**: refund errors → `422 refund_not_allowed`
  (`ErrInvalidRefund`, `ErrRefundExceedsOutstanding`).
- **OpenAPI**: new refund path + `RefundRequest` / `RefundResponse` schemas,
  version bumped to `0.3.0`.
- **React + TypeScript frontend** (`frontend/`, React 19 + Vite 7 +
  TanStack Query):
  - `src/api/` — typed client (`fetch` wrapper adding `X-Actor-Type`/`X-Actor-Id`
    and a per-mutation `Idempotency-Key` via `crypto.randomUUID()`), API types
    mirroring the REST contracts, and TanStack Query hooks
    (`useAuthorize`, `useOrder`, `useRefund`, `useCreateCustomer`,
    `useCreateMerchant`, `useCreditPosition`).
  - `src/lib/money.ts` — string-based `dollarsToCents` / `centsToDollars`
    (BigInt), so no float ever touches a monetary value in the UI.
  - Components: onboarding panel (customer + merchant), checkout form,
    order panel (schedule table, partial-refund form, available-credit display).
  - Vite dev proxy `/checkout → :8081`, `/underwriting → :8082` (prefix stripped)
    keeps the browser same-origin with no CORS setup.
  - Strict TypeScript (`strict`, `noUnusedLocals`, `noUnusedParameters`,
    `verbatimModuleSyntax`, no `any`), flat ESLint config.
- **Tests**: table-driven `refundFee` unit tests; ledger integration test
  (partial → full refund, over-refund rejection, derived balances zeroed,
  `REFUNDED` transition); HTTP integration test (refund + idempotent replay +
  422 over-refund + final `REFUNDED`).

### Verification (all green)

| Command | Result |
| --- | --- |
| `go build ./...` | OK |
| `go vet ./...` | OK |
| `golangci-lint run ./...` | 0 issues |
| `go test -race ./internal/...` | ok |
| `go test -race -tags=integration ./tests/...` | 7/7 pass |
| `npm --prefix frontend run lint` | clean |
| `npm --prefix frontend run build` | tsc + vite build OK |

### Implementation-specific clarifications (carry into Phase 4)

1. **Refunds are compensating entries**, never mutations of history: a `REFUND`
   journal entry with `reference_type='order'` tallies prior reversals via
   `SumOrderRefundPostings` (keyed by the customer receivable and fee-revenue
   account ids).
2. **Only outstanding principal is refundable.** `principal − collected −
   refunded > 0` is the refund ceiling; the API returns `422` beyond it. A cash
   disbursement path (refunding already-collected installments) is deliberately
   out of scope until a treasury/settlement model lands.
3. **Fee reversal is clamped to the refunded amount.** Proportional reversal
   (`mulDiv(remainingFee, amount, outstanding)`) normally never exceeds `amount`
   for realistic fee rates; the clamp only matters when an order is nearly
   fully collected, keeping the merchant-payable debit non-negative.
4. **Refund idempotency** relies on the shared `Idempotency-Key` middleware.
   Unlike orders there is no domain-level unique constraint on refunds
   (`journal_entries.idempotency_key` is non-unique), so the crash-between-commit
   and completion caveat from Phase 2 clarification 3 applies.
5. **Frontend is dev-proxy only**: `npm run build` emits static assets, but there
   is no production reverse proxy/serving config yet; the two services must run
   on `:8081`/`:8082` for the dev flow.

### Open items for Phase 4

- **Merchant payout / collection worker**: wire `ClaimDueInstallments` +
  `PayInstallment` into a background worker and emit `MERCHANT_PAYOUT` (deferred
  since Phase 2).
- **End-to-end/integration testcontainers** for the full stack, including
  `--log-bin-trust-function-creators=1` (Phase 1 clarification 1).
- **Real auth** to replace the placeholder `X-Actor-Type`/`X-Actor-Id` headers.
- **Cash disbursement refunds** for already-collected installments if the
  product requires them.

---

## Phase 4 — Automated Testing & CI/CD + Collection Worker ✅ (2026-11-04)

Scope confirmed with the maintainer: the plan's Phase 4 (table-driven unit
tests, testcontainers integration, one Playwright E2E test, GitHub Actions)
**plus** the merchant payout / collection worker deferred since Phase 2
(ADR-003).

### Delivered

- **Collection & payout worker** (ADR-003, `backend/internal/worker`,
  `backend/cmd/worker`):
  - `Worker.Tick` runs one poll cycle: requeue stale claims →
    `ClaimDueInstallments` (`FOR UPDATE SKIP LOCKED`) → gateway capture →
    `PayInstallment` → `SettleMerchantPayout`. `Run` loops on a ticker until the
    context is cancelled; `SetClock` injects a clock for tests.
  - `Gateway` interface + `SimulatedGateway` + `GatewayFunc` adapter stand in for
    a real payment-gateway integration; the locking/persistence/settlement logic
    is exercised regardless.
  - The worker depends on a narrow `Ledger` interface (not the concrete
    repository), so the poll loop is unit-testable without a database.
  - Config via `WORKER_BATCH_SIZE`, `WORKER_POLL_INTERVAL`, `WORKER_STALE_AFTER`;
    `make run-worker`.
- **Delta-based, idempotent merchant settlement**
  (`internal/ledger/payout.go`, `SettleMerchantPayout`): locks the order, derives
  collected principal and the already-settled total (`SumOrderPayoutPostings`),
  and posts `MERCHANT_PAYOUT` (Dr `MERCHANT_PAYABLE`, Cr `CASH_CLEARING`) for the
  **delta** between the merchant's earned share so far
  (`mulDiv(collected, principal−fee, principal)`) and what has already been paid
  out. Worker retries therefore cannot double-pay; the final installment trues up
  to exactly `principal − fee`.
- **Worker crash recovery** (`internal/ledger/installment.go`):
  `RequeueStaleProcessing` returns installments stuck in `PROCESSING` older than
  a cutoff to `SCHEDULED`, so a worker that dies after claiming a batch does not
  strand rows. `FailInstallment` records a gateway failure.
- **Refund now reconciles the pending schedule** (correctness fix found while
  testing the worker): `reconcileScheduleAfterRefund` walks the schedule from the
  tail (newest first), cancelling whole tail installments (`CancelInstallment`)
  and reducing the boundary one (`UpdateInstallmentAmount`) until the refunded
  amount is absorbed. Previously a refund left remaining installments
  `SCHEDULED`, so the worker would later collect an already-refunded portion.
  `PAID` installments are never touched.
- **Integration tests** (`backend/tests`, build tag `integration`):
  - `TestMain` resolves the DSN from `BNPL_TEST_DSN`/`DB_DSN`, or starts a
    throwaway MySQL 8 via testcontainers (`--log-bin-trust-function-creators=1`)
    and applies the migration files itself when the schema is absent.
  - `concurrency_test.go`: concurrent authorizations obey the credit limit (one
    of six contenders wins under the customer row lock); `FOR UPDATE SKIP LOCKED`
    skips a row held by another session and still claims the rest; concurrent full
    refunds admit exactly one; concurrent payout settlements sum to exactly
    `principal − fee`; and a full worker tick drives an order to `COMPLETED` with
    zeroed receivable/payable.
- **Playwright E2E** (`frontend/e2e/checkout.spec.ts`,
  `frontend/playwright.config.ts`): one checkout flow — onboard customer and
  merchant, authorize an order, assert the 3-row installment schedule, issue a
  partial refund (`AUTHORIZED`, remaining 75.00), then refund the remainder
  (`REFUNDED`, remaining 0.00). `playwright.config.ts` starts both Go services
  and Vite as `webServer`s. `data-testid`s added to `OnboardingPanel`,
  `CheckoutForm` and `OrderPanel`; `npm run test:e2e` / `make frontend-e2e`.
- **CI** (`.github/workflows/ci.yml`), three jobs on a `mysql:8.0` service
  container: **backend** (build, `go vet -tags=integration`, golangci-lint, unit
  tests, integration tests), **frontend** (lint, typecheck, build) and **e2e**
  (apply migrations with the golang-migrate CLI, then Playwright with the
  chromium browser).
- **Docs/tooling**: `README.md` (worker, testing and CI sections, `cmd/worker`
  in the layout), `.env.example` (`WORKER_*`), `Makefile` (`run-worker`,
  `frontend-e2e`).

### Verification (all green)

| Command | Result |
| --- | --- |
| `go build ./...` | OK |
| `go vet -tags=integration ./...` | OK |
| `golangci-lint run ./...` | 0 issues |
| `go test -race ./internal/...` | ok |
| `go test -race -count=1 -tags=integration ./tests/` (env MySQL) | ok |
| `go test -race -count=1 -tags=integration ./tests/` (testcontainers) | ok |
| `npm --prefix frontend run lint` | clean |
| `npm --prefix frontend run build` | tsc + vite build OK |
| `npx playwright test` (services + Vite + migrated DB) | 1 passed |

### Implementation-specific clarifications (carry into Phase 5)

1. **CI cannot override the service-container command**, so the required
   `log_bin_trust_function_creators` flag is set at runtime via
   `docker exec <container> mysql -e "SET GLOBAL log_bin_trust_function_creators=1"`
   before migrations/tests. Locally the compose file and the testcontainers
   `WithCmd` set it on startup instead.
2. **Settlement is delta-based, not amount-per-installment.** `MERCHANT_PAYOUT`
   pays the difference between the merchant's earned share of *collected*
   principal and what it has already been paid. This makes retries idempotent
   without a payout ledger table; the trade-off is that a payout journal entry is
   not one-to-one with an installment.
3. **The worker's payment gateway is simulated.** Only the ledger effects are
   real; swapping in a live gateway means implementing `worker.Gateway`.
4. **Testcontainers needs the same MySQL flags as compose** (Phase 1
   clarification 1) and the `multiStatements=true` DSN to apply the migration
   files, which the test harness now does itself — so `make migrate-up` is not a
   precondition for the integration suite.
5. **Go ignores environment variables for test caching**; switching between the
   env DSN and testcontainers requires `-count=1`.
6. **CASH_CLEARING is a global SYSTEM account**, so per-test assertions on its
   balance are unsafe against a shared database; the worker test asserts on
   order-scoped accounts (receivable/payable) instead.
7. **Refunds and worker collection now interact safely**, but the refund path
   cancels/reduces only `SCHEDULED`/`PROCESSING` installments; a refund racing a
   *capture already in flight* is resolved by the order lock and the derived
   (never stored) outstanding balance.

### Open items for Phase 5

- **Real payment-gateway integration** for `worker.Gateway` (retries, provider
  webhooks, idempotency keys per capture).
- **Retry/backoff policy for failed installments** — `FailInstallment` currently
  records a terminal `FAILED` with `last_error`; dunning and late-fee entries
  (`LATE_FEE_REVENUE` is seeded but unused) are not implemented.
- **Real authentication** to replace the placeholder `X-Actor-Type`/`X-Actor-Id`
  headers.
- **Cash disbursement refunds** for already-collected installments if the product
  requires them.
- **Observability**: the worker logs per-installment events with `trace_id`;
  metrics (`slog` counters only today) and alerting are still open.
