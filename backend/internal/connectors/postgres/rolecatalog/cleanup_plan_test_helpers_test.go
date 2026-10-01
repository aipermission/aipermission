package rolecatalog

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

type privilegeRows struct {
	pgx.Rows
	values  [][3]string
	index   int
	closed  bool
	scanErr error
	err     error
}

func (rows *privilegeRows) Close() { rows.closed = true }
func (rows *privilegeRows) Next() bool {
	if rows.index >= len(rows.values) {
		return false
	}
	rows.index++
	return true
}
func (rows *privilegeRows) Err() error { return rows.err }
func (rows *privilegeRows) Scan(destination ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	for column, value := range rows.values[rows.index-1] {
		*destination[column].(*string) = value
	}
	return nil
}

func cleanupTransaction(t *testing.T, targets ...[3]string) *transaction {
	t.Helper()
	tx := validTransaction(t, testRecord())
	tx.rows[cleanupScopeQuery] = func(destination ...any) error {
		*destination[0].(*bool) = false
		return nil
	}
	tx.queryRows = &privilegeRows{values: targets}
	return tx
}
