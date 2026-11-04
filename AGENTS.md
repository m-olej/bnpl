# BNPL Split-Payment Engine - AI Assistant Guidelines (`CLAUDE.md`)

This repository implements an idempotent, double-entry financial ledger and multi-service BNPL checkout platform built with Go, MySQL 8, and React (TypeScript).

---

## 1. Non-Negotiable Financial Domain Invariants

1. **Absolute Currency Safety (No Floats):**
   - NEVER use `float32`, `float64`, or floating-point arithmetic for monetary calculations in Go or SQL.
   - All monetary amounts MUST be represented as 64-bit signed integers representing cents/minor units (`int64` / SQL `BIGINT`).
   - Rounding remainders on split installments (e.g., $100.00 / 3) MUST be allocated deterministically (the remainder cent goes to the first installment).

2. **Double-Entry Ledger Invariant:**
   - No row in `accounts` may have a mutable running `balance` column updated directly. Balances are derived projections.
   - Every financial transaction consists of at least two `postings`.
   - The algebraic sum of all postings within a single transaction MUST equal zero: `Sum(Debit) - Sum(Credit) = 0`.
   - Postings and Journal Entries are strictly append-only (immutable). Modifications require compensating reversal entries.

3. **Concurrency & Locking:**
   - Any transaction modifying user limits, balances, or claims MUST acquire explicit pessimistic locks (`SELECT ... FOR UPDATE`) in consistent lock-ordering (sorted by primary key) to avoid deadlocks.
   - Distributed worker polling for due installments MUST use `SELECT ... FOR UPDATE SKIP LOCKED`.

---

## 2. Tech Stack & Standards

- **Go (Backend):**
  - Standard library `net/http` or `chi` router.
  - Database access via type-safe code generated with `sqlc`. Do NOT introduce arbitrary dynamic ORMs (`gorm`) unless explicitly requested.
  - Logging via `log/slog` with structured JSON attributes (`trace_id`, `user_id`, `idempotency_key`).
  - Error wrapping using `fmt.Errorf("...: %w", err)`. Explicitly separate domain sentinel errors (`ErrAccountLocked`, `ErrInsufficientCredit`) from transport/database errors.

- **MySQL 8.0+ (Database):**
  - Engine: InnoDB.
  - Strict isolation: Read Committed or Repeatable Read.
  - Schema migrations managed exclusively via `golang-migrate/migrate`. Raw SQL files located in `backend/migrations`.

- **React + TypeScript (Frontend):**
  - React 19 / TypeScript 5+ via Vite.
  - State management: TanStack Query (React Query) for server state; zustand or native hooks for local state.
  - Strict null checks; no use of `any`. Explicitly type all API response contracts.

---

## 3. Standard Development Workflows & CLI Commands

All Go commands run from `backend/`; all frontend commands run from `frontend/`.
The root `Makefile` wraps these (`make verify`, `make migrate-up`, ...).

- **Lint Backend:** `cd backend && golangci-lint run ./...`
- **Generate SQL Queries:** `cd backend && sqlc generate`
- **Run Unit Tests:** `cd backend && go test -v -race ./internal/...`
- **Run Integration Tests (real MySQL 8):** `cd backend && go test -v -race -tags=integration ./tests/...`
- **Lint Frontend:** `npm --prefix frontend run lint`
- **Build Frontend:** `npm --prefix frontend run build`

---

## 4. Expectations for AI Assistant Generated Code

1. **Table-Driven Tests:** All generated Go logic (especially payment splitting, state machines, and middleware) must include table-driven tests covering edge cases (zero values, negative numbers, overflow boundaries, leap days).
2. **Context Propagation:** All database and external HTTP methods must accept and propagate `context.Context` down to drivers and outbound calls.
3. **Idempotency Adherence:** New mutating API endpoints (`POST`, `PATCH`, `DELETE`) must enforce the `Idempotency-Key` header via the shared middleware.
