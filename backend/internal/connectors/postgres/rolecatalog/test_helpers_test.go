package rolecatalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type rowFunc func(...any) error

func (row rowFunc) Scan(destination ...any) error { return row(destination...) }

type transaction struct {
	pgx.Tx
	lockErr          error
	rollbackErr      error
	rollbacks        int
	rollbackDeadline time.Duration
	rollbackCanceled bool
	statements       []string
	queries          []string
	arguments        [][]any
	rows             map[string]rowFunc
	queryRows        pgx.Rows
	queryErr         error
}

func (tx *transaction) Exec(ctx context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if err := ctx.Err(); err != nil {
		return pgconn.CommandTag{}, err
	}
	tx.statements = append(tx.statements, sql)
	return pgconn.CommandTag{}, tx.lockErr
}

func (tx *transaction) Rollback(ctx context.Context) error {
	tx.rollbacks++
	tx.rollbackCanceled = ctx.Err() != nil
	if deadline, ok := ctx.Deadline(); ok {
		tx.rollbackDeadline = time.Until(deadline)
	}
	return tx.rollbackErr
}

func (tx *transaction) QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row {
	tx.queries = append(tx.queries, sql)
	tx.arguments = append(tx.arguments, arguments)
	if err := ctx.Err(); err != nil {
		return rowFunc(func(...any) error { return err })
	}
	if row, ok := tx.rows[sql]; ok {
		return row
	}
	return rowFunc(func(...any) error { return errors.New("unexpected query") })
}

func (tx *transaction) Query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error) {
	tx.queries = append(tx.queries, sql)
	tx.arguments = append(tx.arguments, arguments)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return tx.queryRows, tx.queryErr
}

type starter struct {
	tx      pgx.Tx
	err     error
	options pgx.TxOptions
}

func (s *starter) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	s.options = options
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.tx, s.err
}

func testRecord() rolejournal.Record {
	return rolejournal.Record{
		Version: 1, Generation: "abcdef0123456789abcdef0123456789", RoleOID: 42, Status: rolejournal.Provisioned,
		Intent: rolejournal.Intent{
			OperationID: "1234567890abcdef1234567890abcdef", RoleName: " My Role ",
			Anchor: rolejournal.Anchor{
				TargetID: 1, AdminProfileID: 2, ContextDigest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				TargetDigest: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
				ClusterID:    "18446744073709551615", DatabaseOID: 12, DatabaseName: " Main DB ",
				SuccessorOID: 10, SuccessorName: " Main Admin ",
			},
		},
	}
}

func validTransaction(t *testing.T, record rolejournal.Record) *transaction {
	t.Helper()
	anchor := record.Intent.Anchor
	return &transaction{rows: map[string]rowFunc{
		anchorQuery: func(destination ...any) error {
			if len(destination) != 5 {
				t.Fatalf("anchor scan fields %d", len(destination))
			}
			*destination[0].(*string) = "-1"
			*destination[1].(*uint32), *destination[2].(*string) = anchor.DatabaseOID, anchor.DatabaseName
			*destination[3].(*uint32), *destination[4].(*string) = anchor.SuccessorOID, anchor.SuccessorName
			return nil
		},
		roleQuery: func(destination ...any) error {
			if len(destination) != 3 {
				t.Fatalf("role scan fields %d", len(destination))
			}
			*destination[0].(*uint32) = record.RoleOID
			*destination[1].(*string), *destination[2].(*string) = record.Intent.RoleName, record.Intent.Marker()
			return nil
		},
	}}
}
