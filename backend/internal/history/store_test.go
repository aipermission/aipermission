package history

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	gatewaydb "github.com/aipermission/aipermission/backend/internal/db"
)

func TestStoreSyncsCanonicalHistoryEntries(t *testing.T) {
	database := openTestDB(t)
	tokenID := insertToken(t, database)
	sshTargetID, sshProfileID := insertTargetProfile(t, database, "ssh", "test-vps", "private_key", "main")
	postgresTargetID, postgresProfileID := insertTargetProfile(t, database, "postgres", "orders-db", "username_password", "readonly")
	_ = sshTargetID
	runtimeProfileID := sshProfileID
	store := NewStore(database)

	commandID := insertCommandRequest(t, database, tokenID, runtimeProfileID)
	if err := store.SyncCommandRequest(context.Background(), commandID); err != nil {
		t.Fatalf("sync command request: %v", err)
	}
	assertHistoryEntry(t, database, SourceCommandRequest, commandID, "ssh", "command", "mcp", "completed", "test-vps", "main")

	actionID := insertConnectorActionRequest(t, database, tokenID, postgresTargetID, postgresProfileID)
	if err := store.SyncConnectorActionRequest(context.Background(), actionID); err != nil {
		t.Fatalf("sync connector action request: %v", err)
	}
	assertHistoryEntry(t, database, SourceConnectorActionRequest, actionID, "postgres", "action", "mcp", "completed", "orders-db", "readonly")
	assertConnectorActionHistoryMetadata(t, database, actionID)

	uiActionID := insertConnectorActionRequest(t, database, tokenID, postgresTargetID, postgresProfileID)
	if _, err := database.Exec(`UPDATE connector_action_requests SET source = 'ui' WHERE id = ?`, uiActionID); err != nil {
		t.Fatalf("mark connector action source: %v", err)
	}
	if err := store.SyncConnectorActionRequest(context.Background(), uiActionID); err != nil {
		t.Fatalf("sync ui connector action request: %v", err)
	}
	assertHistoryEntry(t, database, SourceConnectorActionRequest, uiActionID, "postgres", "action", "ui", "completed", "orders-db", "readonly")

	transferID := insertFileTransfer(t, database, runtimeProfileID)
	if err := store.SyncFileTransfer(context.Background(), transferID); err != nil {
		t.Fatalf("sync file transfer: %v", err)
	}
	assertHistoryEntry(t, database, SourceFileTransfer, transferID, "ssh", "file_transfer", "ui", "completed", "test-vps", "main")
}

func TestStoreDeleteSourceRefRemovesCanonicalHistoryEntry(t *testing.T) {
	database := openTestDB(t)
	_, runtimeProfileID := insertTargetProfile(t, database, "ssh", "test-vps", "private_key", "main")
	store := NewStore(database)
	transferID := insertFileTransfer(t, database, runtimeProfileID)
	if err := store.SyncFileTransfer(context.Background(), transferID); err != nil {
		t.Fatalf("sync file transfer: %v", err)
	}
	if err := store.DeleteSourceRef(context.Background(), SourceFileTransfer, transferID); err != nil {
		t.Fatalf("delete history entry: %v", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM history_entries WHERE source_ref_type = ? AND source_ref_id = ?`, SourceFileTransfer, transferID).Scan(&count); err != nil {
		t.Fatalf("count history entries: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deleted history entry, got %d rows", count)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "history.db"), "test-password")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func insertToken(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES ('codex', 'hash', 'aip_xxx', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("token id: %v", err)
	}
	return id
}

func insertTargetProfile(t *testing.T, database *sql.DB, connectorKind string, targetName string, profileKind string, profileLabel string) (int64, int64) {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO connector_targets (connector_kind, name, config_json, created_at, updated_at)
		VALUES (?, ?, '{"host":"127.0.0.1"}', datetime('now'), datetime('now'))`,
		connectorKind,
		targetName,
	)
	if err != nil {
		t.Fatalf("insert target: %v", err)
	}
	targetID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("target id: %v", err)
	}
	result, err = database.Exec(`
		INSERT INTO connector_credential_profiles (
			target_id, connector_kind, kind, label, public_json, encrypted_secret_json, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, '{"username":"readonly"}', 'encrypted', datetime('now'), datetime('now'))`,
		targetID,
		connectorKind,
		profileKind,
		profileLabel,
	)
	if err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	profileID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("profile id: %v", err)
	}
	return targetID, profileID
}

func insertCommandRequest(t *testing.T, database *sql.DB, tokenID int64, runtimeProfileID int64) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO command_requests (
			token_id, runtime_profile_id, command, reason, status, stdout, exit_code, source, created_at, completed_at
		)
		VALUES (?, ?, 'hostname', 'smoke', 'completed', 'test-vps', 0, 'mcp', datetime('now'), datetime('now'))`,
		tokenID,
		runtimeProfileID,
	)
	if err != nil {
		t.Fatalf("insert command request: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("command request id: %v", err)
	}
	return id
}

func insertConnectorActionRequest(t *testing.T, database *sql.DB, tokenID int64, targetID int64, profileID int64) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO connector_action_requests (
			token_id, target_id, profile_id, connector_kind, action_name, title, summary,
			preview_json, status, reason, input_json, output_json, display_text,
			created_at, completed_at
		)
		VALUES (?, ?, ?, 'postgres', 'get_tables', 'List Postgres tables', 'List visible public tables',
			'{"schema":"public"}', 'completed', 'list tables', '{}', '{"tables":[]}', 'no tables',
			datetime('now'), datetime('now'))`,
		tokenID,
		targetID,
		profileID,
	)
	if err != nil {
		t.Fatalf("insert connector action request: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("connector action request id: %v", err)
	}
	return id
}

func assertConnectorActionHistoryMetadata(t *testing.T, database *sql.DB, sourceRefID int64) {
	t.Helper()
	var title string
	var summary string
	var previewJSON string
	if err := database.QueryRow(`
		SELECT title, summary, preview_json
		FROM history_entries
		WHERE source_ref_type = ? AND source_ref_id = ?`,
		SourceConnectorActionRequest,
		sourceRefID,
	).Scan(&title, &summary, &previewJSON); err != nil {
		t.Fatalf("read connector action metadata: %v", err)
	}
	if title != "List Postgres tables" || summary != "List visible public tables" || previewJSON != `{"schema":"public"}` {
		t.Fatalf("connector action metadata mismatch: title=%q summary=%q preview=%q", title, summary, previewJSON)
	}
}

func insertFileTransfer(t *testing.T, database *sql.DB, runtimeProfileID int64) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO file_transfers (
			runtime_profile_id, direction, source, status, remote_path, file_name, size_bytes,
			transferred_bytes, created_at, started_at, completed_at, updated_at
		)
		VALUES (?, 'download', 'ui', 'completed', '/home/report.txt', 'report.txt', 42, 42, datetime('now'), datetime('now'), datetime('now'), datetime('now'))`,
		runtimeProfileID,
	)
	if err != nil {
		t.Fatalf("insert file transfer: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("file transfer id: %v", err)
	}
	return id
}

func assertHistoryEntry(t *testing.T, database *sql.DB, sourceRefType string, sourceRefID int64, connectorKind string, activityType string, source string, status string, targetName string, profileLabel string) {
	t.Helper()
	var gotConnectorKind string
	var gotActivityType string
	var gotSource string
	var gotStatus string
	var gotTargetName string
	var gotProfileLabel string
	if err := database.QueryRow(`
		SELECT connector_kind, activity_type, source, status, target_name, profile_label
		FROM history_entries
		WHERE source_ref_type = ? AND source_ref_id = ?`,
		sourceRefType,
		sourceRefID,
	).Scan(&gotConnectorKind, &gotActivityType, &gotSource, &gotStatus, &gotTargetName, &gotProfileLabel); err != nil {
		t.Fatalf("read history entry: %v", err)
	}
	if gotConnectorKind != connectorKind || gotActivityType != activityType || gotSource != source || gotStatus != status || gotTargetName != targetName || gotProfileLabel != profileLabel {
		t.Fatalf(
			"history entry mismatch: got kind=%q activity=%q source=%q status=%q target=%q profile=%q",
			gotConnectorKind,
			gotActivityType,
			gotSource,
			gotStatus,
			gotTargetName,
			gotProfileLabel,
		)
	}
}
