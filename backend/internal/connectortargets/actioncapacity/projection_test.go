package actioncapacity_test

import (
	"database/sql"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
)

func assertProjection(t *testing.T, database *sql.DB) {
	t.Helper()
	var mismatches int
	err := database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM connector_action_requests r
	 LEFT JOIN connector_action_request_usage u ON u.request_id=r.id
	 WHERE u.request_id IS NULL OR u.token_id IS NOT r.token_id OR u.status<>r.status
	 OR u.stored_bytes<>`+actioncapacity.RecordBytesSQL("r.")).Scan(&mismatches)
	if err != nil || mismatches != 0 {
		t.Fatalf("projection mismatches=%d err=%v", mismatches, err)
	}
}

func TestUsageProjectionTracksWritesRollbackDeletionAndTokenIdentity(t *testing.T) {
	database, tokenID, foreignID, ids := capacityFixture(t)
	assertProjection(t, database)
	requestID := ids[connectors.ResultCompleted]
	if _, err := database.ExecContext(t.Context(), `UPDATE connector_action_requests
	 SET output_json=?, display_text=?, completed_at=datetime('now'), status='running' WHERE id=?`,
		`{"data":"value"}`, "a\x00\u00e9", requestID); err != nil {
		t.Fatal(err)
	}
	assertProjection(t, database)
	before, err := actioncapacity.Measure(t.Context(), database, tokenID, 0)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), `UPDATE connector_action_requests SET output_json=output_json || 'extra' WHERE id=?`, requestID); err != nil {
		t.Fatal(err)
	}
	within, err := actioncapacity.Measure(t.Context(), tx, tokenID, 0)
	if err != nil || within.Bytes != before.Bytes+5 {
		t.Fatalf("transaction usage=%#v before=%#v err=%v", within, before, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	after, err := actioncapacity.Measure(t.Context(), database, tokenID, 0)
	if err != nil || after != before {
		t.Fatalf("rollback usage=%#v before=%#v err=%v", after, before, err)
	}
	if _, err := database.ExecContext(t.Context(), `DELETE FROM connector_action_requests WHERE id=?`, requestID); err != nil {
		t.Fatal(err)
	}
	assertProjection(t, database)
	var remaining int
	if err := database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM connector_action_request_usage WHERE request_id=?`, requestID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("deleted projection remains=%d err=%v", remaining, err)
	}
	if _, err := database.ExecContext(t.Context(), `DELETE FROM api_tokens WHERE id=?`, tokenID); err != nil {
		t.Fatal(err)
	}
	assertProjection(t, database)
	after, err = actioncapacity.Measure(t.Context(), database, tokenID, foreignID)
	if err != nil || after != (actioncapacity.Usage{}) {
		t.Fatalf("deleted token usage=%#v err=%v", after, err)
	}
}
