package console

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/gorilla/websocket"
)

func insertConsoleTestSSHProfile(t *testing.T, database *sql.DB, name string, host string, port int) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	targetResult, err := database.Exec(`
		INSERT INTO connector_targets (project_id, connector_kind, name, config_json, created_at, updated_at)
		VALUES ((SELECT id FROM projects WHERE slug = 'ungrouped' AND status = 'active'), 'ssh', ?, ?, ?, ?)`,
		name,
		`{"host":"`+host+`","port":`+strconv.Itoa(port)+`}`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert connector target: %v", err)
	}
	targetID, err := targetResult.LastInsertId()
	if err != nil {
		t.Fatalf("read target id: %v", err)
	}
	profileResult, err := database.Exec(`
		INSERT INTO connector_credential_profiles (target_id, connector_kind, kind, label, public_json, encrypted_secret_json, created_at, updated_at)
		VALUES (?, 'ssh', 'private_key', 'root', '{"username":"root","ssh_key_id":0}', '', ?, ?)`,
		targetID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert connector profile: %v", err)
	}
	profileID, err := profileResult.LastInsertId()
	if err != nil {
		t.Fatalf("read profile id: %v", err)
	}
	surfaceResult, err := database.Exec(`
		INSERT INTO connector_runtime_surfaces (
			connector_kind, target_id, profile_id, capability_kind, label, created_at, updated_at
		)
		VALUES ('ssh', ?, ?, 'live_console', 'root terminal', ?, ?)`,
		targetID,
		profileID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert runtime surface: %v", err)
	}
	runtimeID, err := surfaceResult.LastInsertId()
	if err != nil {
		t.Fatalf("read runtime surface id: %v", err)
	}
	return runtimeID
}

func newManualHistoryTestSession(t *testing.T) (*sql.DB, *Manager, *managedConsoleSession) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Format(time.RFC3339)
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-1", "127.0.0.1", 22)
	sessionResult, err := database.Exec(`
		INSERT INTO console_sessions (runtime_id, name, status, cols, rows, created_at, updated_at)
		VALUES (?, 'manual', 'connected', 120, 32, ?, ?)`,
		runtimeID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert console session: %v", err)
	}
	sessionID, err := sessionResult.LastInsertId()
	if err != nil {
		t.Fatalf("read session id: %v", err)
	}
	manager := NewManager(database, nil, nil)
	session := &managedConsoleSession{
		id:         sessionID,
		runtimeID:  runtimeID,
		generation: sessionID,
		principal:  testExecutionPrincipal(),
		manager:    manager,
		status:     "connected",
		clients:    map[*websocket.Conn]*sync.Mutex{},
	}
	return database, manager, session
}

func testExecutionPrincipal() executionprincipal.Principal {
	principal, err := executionprincipal.LocalOperator("test-workspace", "test-runtime")
	if err != nil {
		panic(err)
	}
	return principal
}

func readManualHistoryRow(t *testing.T, database *sql.DB) manualHistoryRow {
	t.Helper()
	var row manualHistoryRow
	if err := database.QueryRow(`
		SELECT source, command, status, tracking_reason, stdout, session_id
		FROM command_requests
		ORDER BY id DESC
		LIMIT 1`,
	).Scan(&row.source, &row.command, &row.status, &row.trackingReason, &row.stdout, &row.sessionID); err != nil {
		t.Fatalf("read manual history row: %v", err)
	}
	return row
}

func readManualHistoryRows(t *testing.T, database *sql.DB) []manualHistoryRow {
	t.Helper()
	rows, err := database.Query(`
		SELECT source, command, status, tracking_reason, stdout, session_id
		FROM command_requests
		WHERE source = 'manual'
		ORDER BY id DESC`)
	if err != nil {
		t.Fatalf("read manual history rows: %v", err)
	}
	defer rows.Close()

	items := []manualHistoryRow{}
	for rows.Next() {
		var row manualHistoryRow
		if err := rows.Scan(&row.source, &row.command, &row.status, &row.trackingReason, &row.stdout, &row.sessionID); err != nil {
			t.Fatalf("scan manual history row: %v", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate manual history rows: %v", err)
	}
	return items
}

func waitForManualHistoryStatus(t *testing.T, database *sql.DB, status string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		var row manualHistoryRow
		err := database.QueryRow(`
			SELECT source, command, status, tracking_reason, stdout, session_id
			FROM command_requests
			WHERE source = 'manual'
			ORDER BY id DESC
			LIMIT 1`,
		).Scan(&row.source, &row.command, &row.status, &row.trackingReason, &row.stdout, &row.sessionID)
		if err == nil && row.status == status {
			return
		}
		if time.Now().After(deadline) {
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("read manual history status: %v", err)
			}
			t.Fatalf("manual history status did not become %q, latest row %#v", status, row)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertManualHistoryCount(t *testing.T, database *sql.DB, expected int) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM command_requests WHERE source = 'manual'`).Scan(&count); err != nil {
		t.Fatalf("count manual history rows: %v", err)
	}
	if count != expected {
		t.Fatalf("expected %d manual history rows, got %d", expected, count)
	}
}

func insertStaleManualRunningRow(t *testing.T, database *sql.DB, session *managedConsoleSession, command string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Exec(`
		INSERT INTO command_requests (runtime_id, source, command, reason, status, tracking_reason, session_id, created_at)
		VALUES (?, 'manual', ?, 'manual console command', 'running', 'manual_output_not_tracked', ?, ?)`,
		session.runtimeID,
		command,
		session.id,
		now,
	); err != nil {
		t.Fatalf("insert stale manual running row: %v", err)
	}
}

func countManualRunningRows(t *testing.T, database *sql.DB, sessionID int64) int {
	t.Helper()
	var count int
	if err := database.QueryRow(`
		SELECT COUNT(*)
		FROM command_requests
		WHERE source = 'manual' AND status = 'running' AND session_id = ?`,
		sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("count manual running rows: %v", err)
	}
	return count
}
