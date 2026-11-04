// Package idempotency implements the Idempotency-Key protocol described in
// docs/0002: a MySQL-backed pre-flight claim + replay/caching middleware.
package idempotency

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/molej/bnpl/backend/internal/db/sqlc"
)

// Outcome is the result of a pre-flight claim.
type Outcome int

// Claim outcomes.
const (
	// OutcomeAcquired means this caller won the claim and should run the handler.
	OutcomeAcquired Outcome = iota
	// OutcomeReplay means a completed response exists and must be replayed.
	OutcomeReplay
	// OutcomeInFlight means another request holds the claim (return 409).
	OutcomeInFlight
	// OutcomeMismatch means the key was reused with a different payload (422).
	OutcomeMismatch
)

// Replay is a cached HTTP response.
type Replay struct {
	StatusCode int
	Body       []byte
}

// ClaimResult is the outcome of Claim.
type ClaimResult struct {
	Outcome Outcome
	Replay  *Replay
}

// Claimer is the storage contract used by the middleware. MySQLStore is the
// production implementation; tests provide an in-memory fake.
type Claimer interface {
	Claim(ctx context.Context, key, endpoint, requestHash string, ttl time.Duration) (ClaimResult, error)
	Complete(ctx context.Context, key, endpoint string, statusCode int, body []byte) error
}

// MySQLStore is the persistent Claimer.
type MySQLStore struct {
	q *sqlc.Queries
}

// NewMySQLStore builds a store over an open database.
func NewMySQLStore(db *sql.DB) *MySQLStore {
	return &MySQLStore{q: sqlc.New(db)}
}

// Claim attempts to insert a PROCESSING record. On a duplicate key it inspects
// the existing row to decide between replay, in-flight and mismatch.
func (s *MySQLStore) Claim(ctx context.Context, key, endpoint, requestHash string, ttl time.Duration) (ClaimResult, error) {
	_, err := s.q.InsertIdempotencyKey(ctx, sqlc.InsertIdempotencyKeyParams{
		KeyValue:    key,
		Endpoint:    endpoint,
		RequestHash: requestHash,
		ExpiresAt:   time.Now().UTC().Add(ttl),
	})
	if err == nil {
		return ClaimResult{Outcome: OutcomeAcquired}, nil
	}
	if !isDuplicateKey(err) {
		return ClaimResult{}, fmt.Errorf("claiming idempotency key: %w", err)
	}

	row, err := s.q.GetIdempotencyKey(ctx, sqlc.GetIdempotencyKeyParams{
		KeyValue: key,
		Endpoint: endpoint,
	})
	if err != nil {
		return ClaimResult{}, fmt.Errorf("loading idempotency key: %w", err)
	}

	if row.RequestHash != requestHash {
		return ClaimResult{Outcome: OutcomeMismatch}, nil
	}
	if row.Status == sqlc.IdempotencyKeysStatusCOMPLETED {
		return ClaimResult{
			Outcome: OutcomeReplay,
			Replay: &Replay{
				StatusCode: int(row.ResponseStatus.Int32),
				Body:       []byte(row.ResponseBody.String),
			},
		}, nil
	}
	return ClaimResult{Outcome: OutcomeInFlight}, nil
}

// Complete records the response and marks the key COMPLETED.
func (s *MySQLStore) Complete(ctx context.Context, key, endpoint string, statusCode int, body []byte) error {
	if err := s.q.CompleteIdempotencyKey(ctx, sqlc.CompleteIdempotencyKeyParams{
		ResponseStatus: sql.NullInt32{Int32: int32(statusCode), Valid: true},
		ResponseBody:   sql.NullString{String: string(body), Valid: true},
		KeyValue:       key,
		Endpoint:       endpoint,
	}); err != nil {
		return fmt.Errorf("completing idempotency key: %w", err)
	}
	return nil
}

// PurgeExpired removes keys that expired before the given time and returns the
// number of rows deleted. Intended for a maintenance worker.
func (s *MySQLStore) PurgeExpired(ctx context.Context, before time.Time) (int64, error) {
	n, err := s.q.DeleteExpiredIdempotencyKeys(ctx, before)
	if err != nil {
		return 0, fmt.Errorf("purging expired idempotency keys: %w", err)
	}
	return n, nil
}

func isDuplicateKey(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}
