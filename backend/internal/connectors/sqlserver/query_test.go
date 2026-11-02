package sqlserverconnector

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"time"
)

type resultFixture struct {
	columns, types   []string
	data             [][]driver.Value
	offset           int
	queryErr, rowErr error
	setup            string
}

func (r *resultFixture) Connect(context.Context) (driver.Conn, error) { return r, nil }
func (r *resultFixture) Driver() driver.Driver                        { return r }
func (r *resultFixture) Open(string) (driver.Conn, error)             { return r, nil }
func (r *resultFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (r *resultFixture) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (r *resultFixture) Close() error              { return nil }
func (r *resultFixture) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	r.setup = query
	return driver.RowsAffected(0), nil
}
func (r *resultFixture) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if r.queryErr != nil {
		return nil, r.queryErr
	}
	return r, nil
}
func (r *resultFixture) Columns() []string                       { return r.columns }
func (r *resultFixture) ColumnTypeDatabaseTypeName(i int) string { return r.types[i] }
func (r *resultFixture) Next(values []driver.Value) error {
	if r.rowErr != nil {
		return r.rowErr
	}
	if r.offset == len(r.data) {
		return io.EOF
	}
	copy(values, r.data[r.offset])
	r.offset++
	return nil
}

func TestBoundedSQLResultsAndNumericFidelity(t *testing.T) {
	fixture := &resultFixture{columns: []string{"id", "amount", "id", "binary"}, types: []string{"BIGINT", "DECIMAL", "INT", "VARBINARY"}, data: [][]driver.Value{
		{int64(math.MaxInt64), []byte("12345678901234567890.123456"), int64(2), []byte{0xff, 0x00}},
		{int64(1), []byte("0.1"), int64(3), nil},
	}}
	db := sql.OpenDB(fixture)
	defer db.Close()
	result, err := queryRows(t.Context(), db, "SELECT ...", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowCount != 1 || !result.Truncated || fixture.setup != "SET ROWCOUNT 2" || result.Columns[2] != "id_2" {
		t.Fatalf("%#v", result)
	}
	if result.Rows[0]["id"] != "9223372036854775807" || result.Rows[0]["amount"] != "12345678901234567890.123456" || result.Rows[0]["binary"] != "0xff00" {
		t.Fatalf("lost value identity: %#v", result.Rows)
	}
}

func TestResultCellOutputAndColumnLimits(t *testing.T) {
	fixture := &resultFixture{columns: []string{"value"}, types: []string{"NVARCHAR"}, data: [][]driver.Value{{strings.Repeat("x", maxCellBytes+100)}}}
	db := sql.OpenDB(fixture)
	defer db.Close()
	result, err := queryRows(t.Context(), db, "SELECT ...", 10, false)
	if err != nil || !result.Truncated {
		t.Fatalf("%#v %v", result, err)
	}
	encoded, _ := json.Marshal(result.ToMap(maxOutputBytes, nil))
	if len(encoded) > maxOutputBytes {
		t.Fatal("output over budget")
	}
	fixture.columns = make([]string, 257)
	fixture.types = make([]string, 257)
	if _, err := queryRows(t.Context(), db, "SELECT ...", 1, false); err == nil {
		t.Fatal("accepted excessive columns")
	}
}

func TestQueryFailuresAreNotSuccessfulPartialResults(t *testing.T) {
	for _, fixture := range []*resultFixture{
		{queryErr: errors.New("fixture query failure")},
		{columns: []string{"x"}, types: []string{"INT"}, rowErr: errors.New("fixture stream failure")},
	} {
		db := sql.OpenDB(fixture)
		if _, err := queryRows(t.Context(), db, "SELECT 1", 10, false); err == nil {
			t.Fatal("failure hidden")
		}
		_ = db.Close()
	}
	if normalizeValue(nil, "INT") != nil || normalizeValue([]byte(nil), "VARBINARY") != nil || normalizeValue(int64(1), "BIGINT") != int64(1) {
		t.Fatal("null/small integer changed")
	}
	if value := normalizeValue(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), "DATETIME2"); value != "2026-11-02T00:00:00" {
		t.Fatal(value)
	}
}
