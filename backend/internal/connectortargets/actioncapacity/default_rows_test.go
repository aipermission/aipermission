package actioncapacity_test

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
)

func TestDefaultAdmissionUsesActualRowCeiling(t *testing.T) {
	database, tokenID, _, ids := capacityFixture(t)
	// Seed canonical storage directly: this tests aggregation, not application admission.
	result, err := database.ExecContext(t.Context(), `WITH RECURSIVE sequence(n) AS (
		SELECT 1 UNION ALL SELECT n + 1 FROM sequence WHERE n < ?
	)
	INSERT INTO connector_action_requests (
		token_id, target_id, profile_id, connector_kind, action_name, status, created_at
	)
	SELECT seed.token_id, seed.target_id, seed.profile_id, seed.connector_kind,
		seed.action_name, seed.status, seed.created_at
	FROM sequence CROSS JOIN connector_action_requests seed WHERE seed.id = ?`,
		20001-5, ids[connectors.ResultCompleted])
	if err != nil {
		t.Fatal(err)
	}
	if count, err := result.RowsAffected(); err != nil || count != 20001-5 {
		t.Fatalf("seed rows=%d err=%v", count, err)
	}
	usage, err := actioncapacity.Measure(t.Context(), database, tokenID, 0)
	if err != nil || usage.Rows != 20001 || usage.Bytes >= 256<<20 || usage.Running >= 4 {
		t.Fatalf("row-only excess not isolated: %#v %v", usage, err)
	}
	allowed, err := actioncapacity.WithinDefault(t.Context(), database, tokenID, 0)
	if err != nil || allowed {
		t.Fatalf("excess default rows allowed=%v err=%v", allowed, err)
	}
	_, err = database.ExecContext(t.Context(), `DELETE FROM connector_action_requests
		WHERE id = (SELECT MAX(id) FROM connector_action_requests WHERE token_id = ?)`, tokenID)
	if err != nil {
		t.Fatal(err)
	}
	usage, err = actioncapacity.Measure(t.Context(), database, tokenID, 0)
	if err != nil || usage.Rows != 20000 {
		t.Fatalf("exact row count=%#v err=%v", usage, err)
	}
	allowed, err = actioncapacity.WithinDefault(t.Context(), database, tokenID, 0)
	if err != nil || !allowed {
		t.Fatalf("exact default rows rejected=%v err=%v", allowed, err)
	}
}
