package sqldb

import (
	"context"
	"database/sql"
	"fmt"
)

type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type starter interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

func Transaction(ctx context.Context, executor Executor, options *sql.TxOptions, label string) (Executor, func() error, func(), error) {
	transactionStarter, ok := executor.(starter)
	if !ok {
		return executor, func() error { return nil }, func() {}, nil
	}
	tx, err := transactionStarter.BeginTx(ctx, options)
	if err != nil {
		if label == "" {
			return nil, nil, nil, err
		}
		return nil, nil, nil, fmt.Errorf("begin %s: %w", label, err)
	}
	return tx, tx.Commit, func() { _ = tx.Rollback() }, nil
}
