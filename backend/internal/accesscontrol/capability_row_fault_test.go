package accesscontrol

import (
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/testkit/rowfault"
)

func TestExistingCapabilityStatesRejectsRowIterationFailure(t *testing.T) {
	database := rowfault.Open(func(string) rowfault.Result {
		return rowfault.Result{IterationError: rowfault.ErrIteration}
	})
	defer database.Close()
	_, err := existingCapabilityStates(t.Context(), database, 1)
	if !errors.Is(err, rowfault.ErrIteration) {
		t.Fatalf("row iteration error = %v, want injected error", err)
	}
}
