package postgresconnector

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestQueryRowsPreservesBinaryAndTextThroughBuilder(t *testing.T) {
	rows := &queryValueRows{
		fields: []pgconn.FieldDescription{{Name: "binary", DataTypeOID: pgtype.ByteaOID}, {Name: "text", DataTypeOID: pgtype.TextOID}},
		values: [][]any{{[]byte{0xff, 0x00, 0x41}, "\ufffd"}, {[]byte{}, `\xff`}, {nil, "text"}},
	}
	tx := &queryValueTransaction{rows: rows}
	output, err := queryRows(t.Context(), tx, "fixture query", 5, "unchanged argument")
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{{"binary": `\xff0041`, "text": "\ufffd"}, {"binary": `\x`, "text": `\xff`}, {"binary": nil, "text": "text"}}
	if !reflect.DeepEqual(output.Rows, want) || output.Truncated || output.RowCount != 3 || !rows.closed {
		t.Fatalf("query output=%#v closed=%v", output, rows.closed)
	}
	if tx.sql != "fixture query" || !reflect.DeepEqual(tx.arguments, []any{"unchanged argument"}) {
		t.Fatal("query arguments changed before dispatch")
	}
}

func TestQueryRowsClosesBinaryResultsAtLimits(t *testing.T) {
	for _, item := range []struct {
		name string
		rows [][]any
	}{
		{"cell", [][]any{{[]byte(strings.Repeat("x", maxCellBytes))}}},
		{"rows", [][]any{{[]byte{0x00}}, {[]byte{0x01}}}},
	} {
		t.Run(item.name, func(t *testing.T) {
			rows := &queryValueRows{fields: []pgconn.FieldDescription{{Name: "binary"}}, values: item.rows}
			result, err := queryRows(t.Context(), &queryValueTransaction{rows: rows}, "fixture", 1)
			if err != nil || !result.Truncated || !rows.closed || result.RowCount != 1 {
				t.Fatalf("bounded query=%#v closed=%v err=%v", result, rows.closed, err)
			}
		})
	}
}

func TestQueryRowsPropagatesFailuresWithoutPartialSuccess(t *testing.T) {
	failure := errors.New("fixture failure")
	for _, item := range []struct {
		name        string
		queryError  error
		valuesError error
		rowsError   error
		want        string
	}{
		{"query", failure, nil, nil, "query postgres"},
		{"decode", nil, failure, nil, "read postgres row"},
		{"iteration", nil, nil, failure, "iterate postgres rows"},
	} {
		t.Run(item.name, func(t *testing.T) {
			rows := &queryValueRows{fields: []pgconn.FieldDescription{{Name: "binary"}}, values: [][]any{{[]byte{0xff}}}, valuesError: item.valuesError, rowsError: item.rowsError}
			result, err := queryRows(t.Context(), &queryValueTransaction{rows: rows, queryError: item.queryError}, "fixture", 1)
			if !errors.Is(err, failure) || !strings.Contains(err.Error(), item.want) || len(result.Rows) != 0 {
				t.Fatalf("failure returned partial success: %#v err=%v", result, err)
			}
			if item.queryError == nil && !rows.closed {
				t.Fatal("failed query retained its rows")
			}
		})
	}
}

type queryValueTransaction struct {
	pgx.Tx
	rows       pgx.Rows
	queryError error
	sql        string
	arguments  []any
}

func (tx *queryValueTransaction) Query(_ context.Context, sql string, arguments ...any) (pgx.Rows, error) {
	tx.sql, tx.arguments = sql, arguments
	return tx.rows, tx.queryError
}

type queryValueRows struct {
	pgx.Rows
	fields      []pgconn.FieldDescription
	values      [][]any
	index       int
	closed      bool
	valuesError error
	rowsError   error
}

func (rows *queryValueRows) FieldDescriptions() []pgconn.FieldDescription { return rows.fields }
func (rows *queryValueRows) Close()                                       { rows.closed = true }
func (rows *queryValueRows) Err() error                                   { return rows.rowsError }
func (rows *queryValueRows) Next() bool {
	rows.index++
	return rows.index <= len(rows.values)
}
func (rows *queryValueRows) Values() ([]any, error) {
	return rows.values[rows.index-1], rows.valuesError
}
