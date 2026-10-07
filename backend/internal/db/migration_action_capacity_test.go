package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func removeCapacityProjection(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`DROP TRIGGER project_connector_action_usage_insert`,
		`DROP TRIGGER project_connector_action_usage_update`,
		`DROP TABLE connector_action_request_usage`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCapacityProjectionUpgradeBackfillsWithoutDiscardingRequests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capacity-upgrade.aipdb")
	database, err := OpenEncrypted(path, "CapacityUpgradePassword123")
	if err != nil {
		t.Fatal(err)
	}
	targetID, profileID := insertConnectorTargetAndProfile(t, database)
	if _, err := database.Exec(`INSERT INTO connector_action_requests (
	 target_id, profile_id, connector_kind, action_name, status, output_json, created_at, completed_at
	) VALUES (?, ?, 'capacity_fixture', 'inspect', 'completed', '{"value":"preserved"}', datetime('now'), datetime('now'))`, targetID, profileID); err != nil {
		t.Fatal(err)
	}
	// Recreate the prior schema boundary using the production encrypted open path.
	for _, statement := range []string{
		`DROP TRIGGER project_connector_action_usage_insert`,
		`DROP TRIGGER project_connector_action_usage_update`,
		`DROP TABLE connector_action_request_usage`,
		`DELETE FROM schema_migrations WHERE version=43`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = OpenEncrypted(path, "CapacityUpgradePassword123")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var preserved int
	if err := database.QueryRow(`SELECT COUNT(*) FROM connector_action_requests r
	 JOIN connector_action_request_usage u ON u.request_id=r.id
	 WHERE r.output_json='{"value":"preserved"}' AND u.stored_bytes>0 AND u.status=r.status`).Scan(&preserved); err != nil || preserved != 1 {
		t.Fatalf("backfilled requests=%d err=%v", preserved, err)
	}
	var migrations, triggers int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=43`).Scan(&migrations); err != nil || migrations != 1 {
		t.Fatalf("migration=%d err=%v", migrations, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name LIKE 'project_connector_action_usage_%'`).Scan(&triggers); err != nil || triggers != 2 {
		t.Fatalf("triggers=%d err=%v", triggers, err)
	}
}
