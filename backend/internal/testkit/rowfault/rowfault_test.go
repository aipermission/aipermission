package rowfault

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
)

func TestOpenReportsIterationFailureAfterRows(t *testing.T) {
	database := Open(func(query string) Result {
		if query != "SELECT value" {
			t.Fatalf("query = %q", query)
		}
		return Result{
			Columns:        []string{"value"},
			Rows:           [][]driver.Value{{"first"}},
			IterationError: ErrIteration,
		}
	})
	defer database.Close()

	rows, err := database.QueryContext(context.Background(), "SELECT value")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected first row")
	}
	var value string
	if err := rows.Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "first" || rows.Next() || !errors.Is(rows.Err(), ErrIteration) {
		t.Fatalf("value = %q, iteration error = %v", value, rows.Err())
	}
	if _, err := database.ExecContext(context.Background(), "DELETE FROM records"); err == nil {
		t.Fatal("unexpected mutation succeeded")
	}
}
