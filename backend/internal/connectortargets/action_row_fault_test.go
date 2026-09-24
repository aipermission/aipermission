package connectortargets

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/testkit/rowfault"
)

func TestInvalidateActionRequestsRejectsRowIterationFailure(t *testing.T) {
	database := rowfault.Open(func(string) rowfault.Result {
		return rowfault.Result{
			Columns:        []string{"id", "status", "dispatch_started_at"},
			Rows:           [][]driver.Value{{int64(7), "approval_pending", ""}},
			IterationError: rowfault.ErrIteration,
		}
	})
	defer database.Close()
	_, err := NewStore(database).InvalidateActionRequestsForTarget(t.Context(), InvalidateActionRequestsForTargetInput{TargetID: 1})
	if !errors.Is(err, rowfault.ErrIteration) {
		t.Fatalf("row iteration error = %v, want injected error", err)
	}
}
