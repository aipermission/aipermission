package sqlserverconnector

import (
	"math"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestActionQueryRejectsInvalidNativeRowLimits(t *testing.T) {
	for _, limit := range []any{int64(math.MinInt64), -1, 0, maxRows + 1, int64(math.MaxInt32) + 1, int64(1<<32) + 1, int64(math.MaxInt64), nil, "invalid", 1.5} {
		query, _, rows, err := actionQuery(connectors.PreparedAction{
			ActionName: ActionQueryReadonly,
			Payload:    map[string]any{"sql": "SELECT 1", "max_rows": limit},
		})
		if err == nil || query != "" || rows != 0 {
			t.Fatalf("accepted invalid row limit %v: %q, %d", limit, query, rows)
		}
	}
}

func TestActionQueryPreservesNativeRowLimitBoundaries(t *testing.T) {
	for _, limit := range []int64{1, defaultMaxRows, maxRows} {
		query, _, rows, err := actionQuery(connectors.PreparedAction{
			ActionName: ActionQueryReadonly,
			Payload:    map[string]any{"sql": "SELECT 1", "max_rows": limit},
		})
		if err != nil || query != "SELECT 1" || int64(rows) != limit {
			t.Fatalf("changed valid row limit %d: %q, %d, %v", limit, query, rows, err)
		}
	}
	if _, _, _, err := actionQuery(connectors.PreparedAction{ActionName: "unknown"}); err == nil {
		t.Fatal("unknown action was treated as a SQL query")
	}
}
