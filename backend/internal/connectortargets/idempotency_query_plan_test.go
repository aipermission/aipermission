package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

type idempotencyQueryRecorder struct {
	*sql.DB
	queries []string
	args    [][]any
}

func (recorder *idempotencyQueryRecorder) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	recorder.queries = append(recorder.queries, query)
	recorder.args = append(recorder.args, append([]any(nil), args...))
	return recorder.DB.QueryRowContext(ctx, query, args...)
}

func TestIdempotencyMissUsesPartialIndexBeforeTombstoneLookup(t *testing.T) {
	database := openTargetTestDB(t)
	recorder := &idempotencyQueryRecorder{DB: database}
	store := &Store{db: recorder}
	tokenID := int64(7)
	_, err := store.GetActionRequestByIdempotency(t.Context(), &tokenID, "mcp", " missing-request ")
	if !errors.Is(err, ErrActionRequestNotFound) || len(recorder.queries) != 2 {
		t.Fatalf("missing replay: err=%v queries=%d", err, len(recorder.queries))
	}
	rows, err := database.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+recorder.queries[0], recorder.args[0]...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "; ")
	if !strings.Contains(joined, "USING INDEX idx_connector_action_requests_idempotency") || strings.Contains(joined, "SCAN r") {
		t.Fatalf("idempotency lookup scans retained request payloads instead of the keyed partial index: %s", joined)
	}
}
