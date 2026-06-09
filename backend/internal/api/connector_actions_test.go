package api

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestRuntimePrepareConnectorActionUsesLegacySSHResolver(t *testing.T) {
	database := openAPITestDB(t)
	keyID := insertAPITestSSHKey(t, database, "main")
	serverID := insertAPITestServer(t, database, keyID)
	runtime := &databaseRuntime{database: database}

	prepared, err := runtime.prepareConnectorAction(context.Background(), actions.PrepareRequest{
		Source:     "mcp",
		TargetRef:  connectortargets.SSHTargetRef(serverID),
		ActionName: sshconnector.ActionExec,
		Input:      map[string]any{"command": "uptime"},
		Reason:     "smoke",
		CreatedAt:  time.Date(2026, 6, 9, 12, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("prepare connector action: %v", err)
	}

	if prepared.Action.ConnectorKind != sshconnector.Kind {
		t.Fatalf("connector kind = %q", prepared.Action.ConnectorKind)
	}
	if prepared.Action.TargetRef != connectortargets.SSHTargetRef(serverID) {
		t.Fatalf("target ref = %q", prepared.Action.TargetRef)
	}
	if prepared.Action.ProfileID != keyID {
		t.Fatalf("profile id = %d", prepared.Action.ProfileID)
	}
	if prepared.Action.Payload["command"] != "uptime" {
		t.Fatalf("payload = %#v", prepared.Action.Payload)
	}
}

func openAPITestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "test.db"), "test-password")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func insertAPITestSSHKey(t *testing.T, database *sql.DB, name string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO ssh_keys (name, key_type, public_key, encrypted_private_key, fingerprint, created_at, updated_at)
		VALUES (?, 'ed25519', 'ssh-ed25519 AAAATEST aipermission-test', 'encrypted', 'SHA256:test', ?, ?)`,
		name,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert ssh key: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("ssh key id: %v", err)
	}
	return id
}

func insertAPITestServer(t *testing.T, database *sql.DB, sshKeyID int64) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO servers (name, host, port, username, ssh_key_id, description, created_at, updated_at)
		VALUES ('core-1', '10.0.0.10', 22, 'root', ?, 'test server', ?, ?)`,
		sshKeyID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("server id: %v", err)
	}
	return id
}
