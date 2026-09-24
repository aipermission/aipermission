package projects

import (
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/testkit/rowfault"
)

func TestReplaceTokenScopesRejectsPartialActiveProjectRead(t *testing.T) {
	database := rowfault.Open(func(query string) rowfault.Result {
		switch {
		case strings.Contains(query, "SELECT 1 FROM api_tokens"):
			return rowfault.Result{Columns: []string{"exists"}, Rows: [][]driver.Value{{int64(1)}}}
		case strings.Contains(query, "SELECT id FROM projects"):
			return rowfault.Result{
				Columns: []string{"id"}, Rows: [][]driver.Value{{int64(42)}},
				IterationError: rowfault.ErrIteration,
			}
		default:
			return rowfault.Result{Columns: []string{"id"}}
		}
	})
	defer database.Close()
	_, err := NewStore(database).ReplaceTokenScopes(t.Context(), 1, nil)
	if !errors.Is(err, rowfault.ErrIteration) {
		t.Fatalf("row iteration error = %v, want injected error", err)
	}
}
