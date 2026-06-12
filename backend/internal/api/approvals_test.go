package api

import (
	"context"
	"database/sql"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestCommandRequestPendingTransitionIsSingleUse(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(t.TempDir()+"/test.db", "test-password")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	serverID := insertApprovalTestSSHProfile(t, database)
	result, err := database.Exec(`
		INSERT INTO command_requests (server_id, command, reason, status, created_at)
		VALUES (?, 'ls', 'test', 'pending_approval', ?)`,
		serverID,
		now,
	)
	if err != nil {
		t.Fatalf("insert request: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	server := &Server{}
	runtime := &databaseRuntime{database: database}
	if err := server.markCommandRequestRunning(context.Background(), runtime, id); err != nil {
		t.Fatalf("first transition failed: %v", err)
	}
	if err := server.markCommandRequestRunning(context.Background(), runtime, id); err != errCommandRequestNotPending {
		t.Fatalf("expected second transition to fail with errCommandRequestNotPending, got %v", err)
	}
}

func TestDeclinePendingTransitionIsSingleUse(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(t.TempDir()+"/test.db", "test-password")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	serverID := insertApprovalTestSSHProfile(t, database)
	result, err := database.Exec(`
		INSERT INTO command_requests (server_id, command, reason, status, created_at)
		VALUES (?, 'ls', 'test', 'pending_approval', ?)`,
		serverID,
		now,
	)
	if err != nil {
		t.Fatalf("insert request: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	server := &Server{}
	runtime := &databaseRuntime{database: database}
	if err := server.declineCommandRequest(context.Background(), runtime, id, "no"); err != nil {
		t.Fatalf("first transition failed: %v", err)
	}
	var errorText string
	if err := database.QueryRow(`SELECT error FROM command_requests WHERE id = ?`, id).Scan(&errorText); err != nil {
		t.Fatalf("read declined error: %v", err)
	}
	if errorText != "User declined the command" {
		t.Fatalf("unexpected decline error: %q", errorText)
	}
	if err := server.declineCommandRequest(context.Background(), runtime, id, "no"); err != errCommandRequestNotPending {
		t.Fatalf("expected second transition to fail with errCommandRequestNotPending, got %v", err)
	}
}

func insertApprovalTestSSHProfile(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	targetResult, err := database.Exec(`
		INSERT INTO connector_targets (connector_kind, name, config_json, created_at, updated_at)
		VALUES ('ssh', 'core-1', '{"host":"127.0.0.1","port":22}', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert connector target: %v", err)
	}
	targetID, err := targetResult.LastInsertId()
	if err != nil {
		t.Fatalf("target id: %v", err)
	}
	profileResult, err := database.Exec(`
		INSERT INTO connector_credential_profiles (
			target_id, connector_kind, kind, label, public_json, encrypted_secret_json, created_at, updated_at
		)
		VALUES (?, 'ssh', 'private_key', 'root', '{"username":"root","ssh_key_id":1}', 'encrypted', datetime('now'), datetime('now'))`,
		targetID,
	)
	if err != nil {
		t.Fatalf("insert connector profile: %v", err)
	}
	profileID, err := profileResult.LastInsertId()
	if err != nil {
		t.Fatalf("profile id: %v", err)
	}
	return profileID
}
