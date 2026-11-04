package database

import (
	"context"
	"database/sql"
	"fmt"
)

// TxManager runs callback functions inside a database transaction.
type TxManager struct {
	db *sql.DB
}

// NewTxManager wraps a *sql.DB.
func NewTxManager(db *sql.DB) *TxManager {
	return &TxManager{db: db}
}

// WithTx begins a READ COMMITTED transaction, runs fn and commits on success.
// On error or panic the transaction is rolled back. The context is propagated
// to the driver.
func (m *TxManager) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	tx, err := m.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}
