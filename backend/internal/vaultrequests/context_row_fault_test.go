package vaultrequests

import (
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/testkit/rowfault"
)

func TestStalePendingForContextRejectsRowIterationFailure(t *testing.T) {
	database := rowfault.Open(func(string) rowfault.Result {
		return rowfault.Result{IterationError: rowfault.ErrIteration}
	})
	defer database.Close()
	if err := NewStore(database).StalePendingForContext(t.Context(), 1, 1, "context changed"); !errors.Is(err, rowfault.ErrIteration) {
		t.Fatalf("row iteration error = %v, want injected error", err)
	}
}
