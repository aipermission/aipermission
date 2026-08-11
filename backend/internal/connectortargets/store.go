package connectortargets

import (
	"context"
	"database/sql"
	"fmt"
)

type Store struct {
	db storeDB
}

type storeDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type transactionStarter interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type ValidationError string

func (e ValidationError) Error() string {
	return string(e)
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func NewTxStore(tx *sql.Tx) *Store {
	return &Store{db: tx}
}

func (s *Store) transaction(ctx context.Context, label string) (storeDB, func() error, func(), error) {
	starter, ok := s.db.(transactionStarter)
	if !ok {
		return s.db, func() error { return nil }, func() {}, nil
	}
	tx, err := starter.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("begin %s: %w", label, err)
	}
	return tx, tx.Commit, func() { _ = tx.Rollback() }, nil
}
