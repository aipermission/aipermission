package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
)

func TestSSHTargetRefRoundTrip(t *testing.T) {
	ref := SSHTargetRef(42)
	if ref != "ssh:42" {
		t.Fatalf("ref = %q", ref)
	}
	id, ok := ParseSSHTargetRef(ref)
	if !ok || id != 42 {
		t.Fatalf("parse = %d ok=%v", id, ok)
	}
}

func TestParseSSHTargetRefRejectsInvalidRefs(t *testing.T) {
	for _, ref := range []string{"", "server:1", "ssh:", "ssh:0", "ssh:not-number"} {
		if _, ok := ParseSSHTargetRef(ref); ok {
			t.Fatalf("expected %q to be rejected", ref)
		}
	}
}

func TestResolverMapsLegacySSHServerToConnectorViews(t *testing.T) {
	database := openTargetTestDB(t)
	ctx := context.Background()
	keyID := insertTargetTestSSHKey(t, database, "main")
	serverID := insertTargetTestServer(t, database, keyID)

	resolved, err := NewResolver(database).ResolveActionTarget(ctx, SSHTargetRef(serverID))
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}

	if resolved.Target.ID != serverID || resolved.Target.Ref != SSHTargetRef(serverID) {
		t.Fatalf("unexpected target identity: %#v", resolved.Target)
	}
	if resolved.Target.ConnectorKind != sshconnector.Kind {
		t.Fatalf("connector kind = %q", resolved.Target.ConnectorKind)
	}
	if resolved.Target.Config["host"] != "10.0.0.10" || resolved.Target.Config["port"] != 2222 {
		t.Fatalf("unexpected target config: %#v", resolved.Target.Config)
	}
	if resolved.Target.Config["startup_input_after_connect"] != "q" {
		t.Fatalf("startup input missing: %#v", resolved.Target.Config)
	}

	if resolved.Profile.ID != keyID || resolved.Profile.TargetID != serverID {
		t.Fatalf("unexpected profile identity: %#v", resolved.Profile)
	}
	if resolved.Profile.ConnectorKind != sshconnector.Kind || resolved.Profile.Kind != "private_key" {
		t.Fatalf("unexpected profile kind: %#v", resolved.Profile)
	}
	if resolved.Profile.Public["username"] != "admin" {
		t.Fatalf("username public metadata missing: %#v", resolved.Profile.Public)
	}
	if resolved.Profile.Public["fingerprint"] != "SHA256:test" {
		t.Fatalf("fingerprint public metadata missing: %#v", resolved.Profile.Public)
	}
	if _, exists := resolved.Profile.Public["public_key"]; exists {
		t.Fatalf("public key should not be exposed in credential profile metadata: %#v", resolved.Profile.Public)
	}
}

func TestResolverReturnsNotFoundForMissingOrInvalidTarget(t *testing.T) {
	database := openTargetTestDB(t)
	resolver := NewResolver(database)

	for _, ref := range []string{"ssh:999", "postgres:1", "ssh:bad"} {
		_, err := resolver.ResolveActionTarget(context.Background(), ref)
		if !errors.Is(err, actions.ErrTargetNotFound) {
			t.Fatalf("ResolveActionTarget(%q) error = %v", ref, err)
		}
	}
}

func openTargetTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "test.db"), "correct horse battery staple")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func insertTargetTestSSHKey(t *testing.T, database *sql.DB, name string) int64 {
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

func insertTargetTestServer(t *testing.T, database *sql.DB, sshKeyID int64) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO servers (
			name, host, port, username, ssh_key_id, description,
			startup_input_after_connect, force_shell_command, created_at, updated_at
		)
		VALUES ('core-1', '10.0.0.10', 2222, 'admin', ?, 'NAS gateway', 'q', 'bash -l', ?, ?)`,
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
