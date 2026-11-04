The implementation of the project must be executed sequentially considering the implementation plan and proper git etiquette. Always check implemantation details against project rules (AGENTS.md) and assumptions (docs/adr/*).

1. Phase 1: Schema & Ledger Core: 
    - Set up Docker Compose with MySQL 8. 
    - Write raw SQL migrations (golang-migrate) for the double-entry ledger and installment tables. 
    - Implement the transactional Go repository using sqlc for type-safe SQL.

2. Phase 2: Microservices & Idempotent REST APIs: 
    - Build the Checkout Service and Underwriting Service in Go. 
    - Add middleware for Idempotency-Key, structured JSON logging (slog), and OpenAPI/Swagger specs.

3. Phase 3: React Checkout & Merchant UI: 
    - Build the React frontend with TypeScript, managing server state (via TanStack Query) to trigger checkouts, inspect installment schedules, and issue partial refunds.

4. Phase 4: Automated Testing & CI/CD Pipeline: 
    - Write table-driven Go unit tests, spin up a real MySQL container via testcontainers-go to verify concurrent FOR UPDATE locks
    - add one Playwright E2E checkout test, and wire everything into GitHub Actions.
