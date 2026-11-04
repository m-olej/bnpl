# ADR-001: Immutable Double-Entry Ledger System over Mutable Account Balances

### Context
In a Buy Now, Pay Later (BNPL) platform, the system manages receivables across merchants, customers, and settlement accounts over an extended schedule (e.g., Pay-in-4 across 6 weeks). 

The naive design pattern common in standard e-commerce architectures is a single `balance` column on an `accounts` or `users` table:
```sql
UPDATE accounts SET balance = balance - 5000 WHERE id = 123;
```

This approach presents critical production risks:

- Lack of Auditability: If a balance drifts due to race conditions or software bugs, there is no deterministic record of why the money moved.
- Reconciliation Failure: External banking partners require exact balancing of accounts payable, accounts receivable, and settlement clearing accounts.
- Double-Spend & Contention: High-frequency updates to a single row create massive row-lock contention in MySQL under peak traffic.

## Decision

We will model all financial movements using an immutable Double-Entry Bookkeeping Ledger:

1. We define three relational tables: `accounts`, `journal_entries`, and `postings`.

2. Every business event (e.g., `ORDER_AUTHORIZED`, `INSTALLMENT_PAID`, `MERCHANT_PAYOUT`, `REFUND`) generates an immutable journal_entry with two or more child postings.

3. Every posting records an amount (BIGINT, strictly in cents/minor units) and a direction (DEBIT or CREDIT).

4. Invariant Constraint: Within a single database transaction, the sum of debits must equal the sum of credits:
$$
    \sum \text{Debit} - \sum \text{Credit} = 0
$$

5. Account balances will not be updated via raw writes; they are read as materialized views or calculated by summing postings:

$$
    \text{Balance} = \sum \text{Debits} - \sum \text{Credits}
$$

## Consequences

#### Positive

1. Complete Audit Trail: Every cent is traceable back to a specific timestamp, transaction ID, and trigger event.

2. Trivial Refunds & Adjustments: Rather than editing historical rows, refunds are simply new compensating journal entries.

3. Strict Compliance: Matches regulatory standards for financial institutions and simplifies end-of-day bank reconciliations.

#### Negative / Trade-offs

1. Write Amplification: A single order authorization creates 1 journal_entry and at least 4 postings (Customer Receivable, Merchant Payable, Fee Revenue, Cash Clearing).

2. Read Latency: Summing historical postings on high-volume accounts can degrade performance over time.

3. This requires future mitigation via periodic account snapshots/checkpoints (e.g., weekly closing balances).

### Alternatives Considered

- Event Sourcing with Kafka/EventStore: High architectural complexity and introduces eventual consistency risks in multi-party settlements. An ACID-compliant relational ledger in MySQL ensures absolute consistency inside transactions.
