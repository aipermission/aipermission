package connectortargets_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
)

type targetTestActionResolver struct {
	store *connectortargets.Store
}

func newTargetTestActionResolver(database *sql.DB) targetTestActionResolver {
	return targetTestActionResolver{store: connectortargets.NewStore(database)}
}

func (r targetTestActionResolver) ResolveActionTarget(ctx context.Context, targetRef string) (actions.ResolvedTarget, error) {
	target, profile, err := r.store.ResolveConnectorActionTarget(ctx, targetRef)
	return actions.ResolvedTarget{Target: target, Profile: profile}, err
}

func TestActionServicePreparesSSHExec(t *testing.T) {
	database := openTargetTestDB(t)
	keyID := insertTargetTestSSHKey(t, database, "main")
	store := connectortargets.NewStore(database)
	target, profile := createTargetTestSSHProfile(t, context.Background(), store, keyID, "core-1", "admin", "10.0.0.10", 2222)
	targetRef := connectors.FormatTargetRef("ssh", target.ID, profile.ID)
	registry := newTargetTestRegistry(t)
	service := actions.NewService(registry, newTargetTestActionResolver(database))

	prepared, err := service.Prepare(context.Background(), actions.PrepareRequest{
		Source:     "mcp",
		TargetRef:  targetRef,
		ActionName: sshconnector.ActionExec,
		Input:      map[string]any{"command": "hostname"},
		Reason:     "smoke",
		CreatedAt:  time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("prepare ssh exec: %v", err)
	}

	if prepared.Action.ConnectorKind != sshconnector.Kind {
		t.Fatalf("connector kind = %q", prepared.Action.ConnectorKind)
	}
	if prepared.Action.TargetRef != targetRef {
		t.Fatalf("target ref = %q", prepared.Action.TargetRef)
	}
	if prepared.Action.ProfileID < 1 {
		t.Fatalf("profile id = %d", prepared.Action.ProfileID)
	}
	if prepared.Action.Risk != connectors.RiskWrite {
		t.Fatalf("risk = %q", prepared.Action.Risk)
	}
	if prepared.Action.Payload["command"] != "hostname" {
		t.Fatalf("payload = %#v", prepared.Action.Payload)
	}
}

func TestActionServicePreparesSSHReadConsole(t *testing.T) {
	database := openTargetTestDB(t)
	keyID := insertTargetTestSSHKey(t, database, "main")
	store := connectortargets.NewStore(database)
	target, profile := createTargetTestSSHProfile(t, context.Background(), store, keyID, "core-1", "admin", "10.0.0.10", 2222)
	targetRef := connectors.FormatTargetRef("ssh", target.ID, profile.ID)
	registry := newTargetTestRegistry(t)
	service := actions.NewService(registry, newTargetTestActionResolver(database))

	prepared, err := service.Prepare(context.Background(), actions.PrepareRequest{
		Source:     "mcp",
		TargetRef:  targetRef,
		ActionName: sshconnector.ActionReadConsole,
		Input:      map[string]any{"tail_bytes": 4096},
	})
	if err != nil {
		t.Fatalf("prepare ssh read_console: %v", err)
	}

	if prepared.Action.ProfileID < 1 {
		t.Fatalf("profile id = %d", prepared.Action.ProfileID)
	}
	if prepared.Action.Risk != connectors.RiskRead {
		t.Fatalf("risk = %q", prepared.Action.Risk)
	}
	if prepared.Action.Payload["tail_bytes"] != 4096 {
		t.Fatalf("payload = %#v", prepared.Action.Payload)
	}
}

func newTargetTestRegistry(t *testing.T) *connectors.Registry {
	t.Helper()
	registry := connectors.NewRegistry()
	if err := registry.Register(sshconnector.New()); err != nil {
		t.Fatalf("register ssh connector: %v", err)
	}
	return registry
}

func openTargetTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "test.db"), "correct horse battery staple")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func insertTargetTestSSHKey(t *testing.T, database *sql.DB, name string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO connector_credential_resources (
			connector_kind, resource_kind, name, resource_type, public_data, encrypted_secret, fingerprint, created_at, updated_at
		)
		VALUES ('ssh', 'private_key', ?, 'ed25519', 'ssh-ed25519 AAAATEST aipermission-test', 'encrypted', 'SHA256:test', ?, ?)`,
		name, now, now,
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

func createTargetTestSSHProfile(t *testing.T, ctx context.Context, store *connectortargets.Store, sshKeyID int64, name string, username string, host string, port int) (connectortargets.Target, connectortargets.CredentialProfile) {
	t.Helper()
	target, err := store.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: sshconnector.Kind,
		Name:          name,
		Config: map[string]any{
			"host": host, "port": port, "description": "NAS gateway",
			"startup_input_after_connect": "q", "force_shell_command": "bash -l",
		},
	})
	if err != nil {
		t.Fatalf("create ssh target: %v", err)
	}
	profile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: sshconnector.Kind, Kind: "private_key", Label: username,
		EncryptedSecretJSON: "{}",
		Public: map[string]any{
			"username": username, "ssh_key_id": sshKeyID, "key_name": "main",
			"key_type": "ed25519", "fingerprint": "SHA256:test",
		},
	})
	if err != nil {
		t.Fatalf("create ssh profile: %v", err)
	}
	return target, profile
}
