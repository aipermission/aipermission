package connectortargets

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type Store struct {
	db sqldb.Executor
}

type storeDB = sqldb.Executor

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

func (s *Store) transaction(ctx context.Context, label string) (sqldb.Executor, func() error, func(), error) {
	return sqldb.Transaction(ctx, s.db, nil, label)
}
