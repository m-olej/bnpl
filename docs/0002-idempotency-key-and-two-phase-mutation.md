# ADR-002: Idempotent Payment Requests via Distributed Key Caching in MySQL

## Context
Clients interacting with the BNPL Checkout API (web frontends, merchant e-commerce gateways) operate over inherently unreliable networks. If a client sends `POST /api/v1/checkout/authorize` and experiences a network timeout:
- The backend may have completed the charge and reserved credit.
- The client, receiving a timeout error, will retry the HTTP request.

Without an idempotency layer, this leads to double authorization, duplicate credit reservations, and split transactions.

## Decision
We enforce an `Idempotency-Key` HTTP header on all mutating API endpoints, managed via a dedicated MySQL table `idempotency_keys` wrapped in a Go HTTP middleware.

### Lifecycle Protocol:

1. **Request Intake:** The middleware extracts `Idempotency-Key` and hashes the incoming payload body (SHA-256).

2. **Pre-Flight Claim:** The middleware attempts to insert a record into `idempotency_keys`:
   ```sql
   INSERT INTO idempotency_keys (key_value, endpoint, request_hash, status, created_at, expires_at)
   VALUES (?, ?, ?, 'PROCESSING', NOW(), DATE_ADD(NOW(), INTERVAL 24 HOUR));

   ```
3. **Branching Logic:**
    - *Case A (Unique Insert Success):* The request proceeds to the handler. Upon handler completion, the middleware updates the record to status = 'COMPLETED', saving the HTTP status code and response body JSON.

    - *Case B (Duplicate Key & status = 'COMPLETED'):* The middleware validates that the incoming request_hash matches the saved hash. If matched, it terminates execution immediately and re-emits the cached status code and payload directly to the client.

    - *Case C (Duplicate Key & status = 'PROCESSING'):* A concurrent request with the same key is in-flight. The middleware returns 409 Conflict with a Retry-After: 2 header.

    - *Case D (Payload Mismatch):* If the key exists but the request_hash differs, the middleware returns 422 Unprocessable Entity (key reuse with mutated payload).

## Consequences

#### Positive

1. Guaranteed "Exactly-Once" execution semantics from the perspective of the caller.

2. Immune to merchant double-clicks, automated browser retries, and flaky mobile connections.

#### Negative / Trade-offs

1. Additional database round-trip before executing domain logic.

2. Requires maintenance workers or TTL logic to purge expired idempotency records older than 24–72 hours to control table growth.

### Alternatives Considered

- In-Memory Redis Storage: While faster, Redis introduces operational overhead and a split-brain risk if Redis fails or loses persistence while the MySQL transaction commits. Keeping the idempotency table in the same MySQL database allows combining domain mutations and idempotency completion in a single ACID commit if necessary.
