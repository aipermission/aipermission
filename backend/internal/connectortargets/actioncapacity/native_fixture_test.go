package actioncapacity_test

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
	"github.com/aipermission/aipermission/backend/internal/db/baselineschema"

	_ "github.com/SE-I-T-Digital/go-sqlcipher"
)

func capacityFixture(t testing.TB) (*sql.DB, int64, int64, map[connectors.ResultStatus]int64) {
	t.Helper()
	uri := url.URL{Scheme: "file", Path: filepath.Join(t.TempDir(), "capacity.aipdb"), RawQuery: url.Values{"_key": {"CapacityFixturePassword123"}, "_foreign_keys": {"ON"}}.Encode()}
	database, err := sql.Open("sqlite3", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxOpenConns(1)
	// Exercise the canonical baseline and projection without importing the db
	// migration owner, which consumes actioncapacity itself.
	for _, statement := range baselineschema.Connector() {
		if _, err := database.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, column := range []string{"retry_policy_json", "idempotency_key", "idempotency_identity_hash", "idempotency_scope", "execution_owner", "execution_lease_expires_at", "dispatch_started_at"} {
		if _, err := database.ExecContext(t.Context(), `ALTER TABLE connector_action_requests ADD COLUMN `+column+` TEXT NOT NULL DEFAULT ''`); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range actioncapacity.ProjectionStatements() {
		if _, err := database.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	tokenID := int64(9007199254740993)
	foreignTokenID := int64(9007199254740995)
	for _, id := range []int64{tokenID, foreignTokenID} {
		_, err := database.ExecContext(t.Context(), `INSERT INTO api_tokens (id, name, token_hash, token_prefix, created_at, updated_at)
			VALUES (?, ?, ?, 'aip_capacity', datetime('now'), datetime('now'))`, id, "capacity-test-"+strconv.FormatInt(id, 10), strconv.FormatInt(id, 10))
		if err != nil {
			t.Fatal(err)
		}
	}
	// Root-store admission is covered by parent integration tests. This fixture
	// seeds storage only, without importing the owner that consumes this package.
	targetID := insertCapacityFixtureRecord(t, database, `INSERT INTO connector_targets (
		connector_kind, name, created_at, updated_at
	) VALUES ('capacity_fixture', 'capacity target', datetime('now'), datetime('now'))`)
	profileID := insertCapacityFixtureRecord(t, database, `INSERT INTO connector_credential_profiles (
		target_id, connector_kind, kind, label, created_at, updated_at
	) VALUES (?, 'capacity_fixture', 'operator', 'capacity profile', datetime('now'), datetime('now'))`, targetID)
	const requestSQL = `INSERT INTO connector_action_requests (
		token_id, target_id, profile_id, connector_kind, action_name, status,
		input_json, encrypted_payload_json, approval_context, approval_context_hash, created_at
	) VALUES (?, ?, ?, 'capacity_fixture', 'inspect', ?,
		'{"value":"\u00e9"}', 'opaque-fixture', '{}', 'fixture-hash', datetime('now'))`
	ids := map[connectors.ResultStatus]int64{}
	for _, status := range []connectors.ResultStatus{
		connectors.ResultCompleted, connectors.ResultFailed, connectors.ResultOutcomeUnknown,
		connectors.ResultApprovalPending, connectors.ResultRunning,
	} {
		ids[status] = insertCapacityFixtureRecord(t, database, requestSQL, tokenID, targetID, profileID, string(status))
	}
	foreignID := insertCapacityFixtureRecord(t, database, requestSQL, foreignTokenID, targetID, profileID, string(connectors.ResultRunning))
	return database, tokenID, foreignID, ids
}

func insertCapacityFixtureRecord(t testing.TB, database *sql.DB, query string, args ...any) int64 {
	t.Helper()
	result, err := database.ExecContext(t.Context(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
