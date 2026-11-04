# ADR-003: Distributed Background Settlement Workers Using MySQL `SKIP LOCKED`

## Context
BNPL installments are scheduled across future intervals (e.g., installment #2 due in 14 days, installment #3 in 28 days). A background scheduler must poll for installments whose `due_date <= NOW()` and trigger external payment gateway captures.

In a horizontally scaled deployment where multiple replicas of the Go service run concurrently in Kubernetes or Docker Swarm, multiple worker routines must poll the same `installments` table without:
1. Colliding on the same installment and triggering double charges.
2. Suffering from severe row-lock blocking where Worker B waits on Worker A's lock.

## Decision
We will implement background batch polling using Go goroutines leveraging MySQL 8.0's `SELECT ... FOR UPDATE SKIP LOCKED` primitive.

### Execution Pattern:
```go
tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
if err != nil {
    return err
}
defer tx.Rollback()

// 1. Lock and fetch unreserved, due installments, ignoring rows locked by peers
query := `
    SELECT id, order_id, amount, customer_id
    FROM installments
    WHERE status = 'SCHEDULED' 
      AND due_date <= NOW()
    ORDER BY due_date ASC
    LIMIT 50
    FOR UPDATE SKIP LOCKED;
`
rows, err := tx.QueryContext(ctx, query)
// 2. Mark selected installments as 'PROCESSING'
...
// 3. Commit transaction immediately to release DB lock
tx.Commit()

// 4. Dispatch processing to worker goroutines with gateway circuit breakers
```

## Consequences

#### Positive

1. **Zero Lock Contention:** SKIP LOCKED instructs the InnoDB query engine to silently skip rows already locked by an open transaction on another worker node, eliminating wait states.

2. **Horizontal Scalability:** Adding more Go worker instances immediately scales throughput linearly without requiring external coordination infrastructure (e.g., ZooKeeper, Redis Redlock).

3. **Graceful Failover:** If a worker crashes while holding locks, MySQL automatically rolls back the transaction upon connection drop, freeing the rows for the next worker.

#### Negative / Trade-offs

1. **Out-of-Order Execution:** If high-priority records are locked, a worker will skip directly to lower-priority records, which may slightly alter execution ordering during heavy traffic spikes.

2. **Requires MySQL 8.0+** (not supported on legacy MySQL 5.7).

### Alternatives Considered

- Distributed Redis Lock (Redlock): Prone to clock drift failure modes, adds an extra dependency, and complicates error recovery when worker nodes crash mid-process.

- RabbitMQ / Kafka Delayed Queues: High operational overhead for scheduling events weeks in advance; message brokers are poor persistent schedule stores compared to indexed relational tables.
