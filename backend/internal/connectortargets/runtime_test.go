package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
)

func TestStoreResolvesSSHConnectorProfileToConnectorViews(t *testing.T) {
	database := openTargetTestDB(t)
	ctx := context.Background()
	keyID := insertTargetTestSSHKey(t, database, "main")
	store := NewStore(database)
	target, profile := createTargetTestSSHProfile(t, ctx, store, keyID, "core-1", "admin", "10.0.0.10", 2222)
	targetRef := connectors.FormatTargetRef("ssh", target.ID, profile.ID)

	resolvedTarget, resolvedProfile, err := store.ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}

	if resolvedTarget.ID != target.ID || resolvedTarget.Ref != targetRef {
		t.Fatalf("unexpected target identity: %#v", resolvedTarget)
	}
	if resolvedTarget.ConnectorKind != sshconnector.Kind {
		t.Fatalf("connector kind = %q", resolvedTarget.ConnectorKind)
	}
	if resolvedTarget.Config["host"] != "10.0.0.10" || resolvedTarget.Config["port"] != float64(2222) {
		t.Fatalf("unexpected target config: %#v", resolvedTarget.Config)
	}
	if resolvedTarget.Config["startup_input_after_connect"] != "q" {
		t.Fatalf("startup input missing: %#v", resolvedTarget.Config)
	}

	if resolvedProfile.ID != profile.ID || resolvedProfile.TargetID != target.ID {
		t.Fatalf("unexpected profile identity: %#v", resolvedProfile)
	}
	if resolvedProfile.ConnectorKind != sshconnector.Kind || resolvedProfile.Kind != "private_key" {
		t.Fatalf("unexpected profile kind: %#v", resolvedProfile)
	}
	if resolvedProfile.Public["username"] != "admin" {
		t.Fatalf("username public metadata missing: %#v", resolvedProfile.Public)
	}
	if resolvedProfile.Public["ssh_key_id"].(float64) != float64(keyID) {
		t.Fatalf("ssh_key_id public metadata missing: %#v", resolvedProfile.Public)
	}
	if resolvedProfile.Public["fingerprint"] != "SHA256:test" {
		t.Fatalf("fingerprint public metadata missing: %#v", resolvedProfile.Public)
	}
	if _, exists := resolvedProfile.Public["public_key"]; exists {
		t.Fatalf("public key should not be exposed in credential profile metadata: %#v", resolvedProfile.Public)
	}
}

func TestStoreReturnsNotFoundForMissingOrInvalidActionTarget(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)

	for _, ref := range []string{"ssh:999:1", "postgres:1", "ssh:bad"} {
		_, _, err := store.ResolveConnectorActionTarget(context.Background(), ref)
		if !errors.Is(err, ErrInvalidTargetRef) && !errors.Is(err, ErrTargetNotFound) && !errors.Is(err, ErrTargetProfileNotFound) {
			t.Fatalf("ResolveConnectorActionTarget(%q) error = %v", ref, err)
		}
	}
}

func TestStoreTargetProfileByRuntimeIDUsesRuntimeSurface(t *testing.T) {
	database := openTargetTestDB(t)
	ctx := context.Background()
	keyID := insertTargetTestSSHKey(t, database, "main")
	store := NewStore(database)
	target, profile := createTargetTestSSHProfile(t, ctx, store, keyID, "core-1", "admin", "10.0.0.10", 2222)
	surface, err := store.EnsureRuntimeSurface(ctx, EnsureRuntimeSurfaceInput{
		ConnectorKind:  sshconnector.Kind,
		TargetID:       target.ID,
		ProfileID:      profile.ID,
		CapabilityKind: RuntimeCapabilityLiveConsole,
		Label:          profile.Label,
	})
	if err != nil {
		t.Fatalf("ensure runtime surface: %v", err)
	}

	gotTarget, gotProfile, gotSurface, err := store.TargetProfileByRuntimeID(ctx, surface.ID)
	if err != nil {
		t.Fatalf("target profile by runtime id: %v", err)
	}
	if gotSurface.ID != surface.ID || gotSurface.ProfileID != profile.ID {
		t.Fatalf("unexpected runtime surface: %#v", gotSurface)
	}
	if gotTarget.ID != target.ID || gotProfile.ID != profile.ID || gotProfile.TargetID != target.ID {
		t.Fatalf("unexpected target/profile: target=%#v profile=%#v", gotTarget, gotProfile)
	}
	if gotProfile.Public["username"] != "admin" || gotProfile.Public["ssh_key_id"].(float64) != float64(keyID) {
		t.Fatalf("unexpected credential metadata: %#v", gotProfile.Public)
	}
	contextTarget, contextProfile, contextSurface, err := store.RuntimeContextByRuntimeID(ctx, surface.ID)
	if err != nil {
		t.Fatalf("runtime context snapshot: %v", err)
	}
	if contextTarget.ID != target.ID || contextProfile.ID != profile.ID || contextSurface.ID != surface.ID {
		t.Fatalf("unexpected runtime snapshot: target=%#v profile=%#v surface=%#v", contextTarget, contextProfile, contextSurface)
	}
	if contextProfile.EncryptedSecretJSON != profile.EncryptedSecretJSON || contextProfile.CreatedAt == "" {
		t.Fatalf("runtime snapshot omitted full credential state: %#v", contextProfile)
	}
}

func TestEnsureRuntimeSurfacePreservesRevisionWhenUnchanged(t *testing.T) {
	database := openTargetTestDB(t)
	ctx := context.Background()
	keyID := insertTargetTestSSHKey(t, database, "main")
	store := NewStore(database)
	target, profile := createTargetTestSSHProfile(t, ctx, store, keyID, "core-1", "admin", "10.0.0.10", 2222)
	input := EnsureRuntimeSurfaceInput{
		ConnectorKind:  sshconnector.Kind,
		TargetID:       target.ID,
		ProfileID:      profile.ID,
		CapabilityKind: RuntimeCapabilityLiveConsole,
		Label:          profile.Label,
	}
	surface, err := store.EnsureRuntimeSurface(ctx, input)
	if err != nil {
		t.Fatalf("ensure runtime surface: %v", err)
	}
	const sentinel = "2026-08-12T08:00:00Z"
	if _, err := database.Exec(`UPDATE connector_runtime_surfaces SET updated_at = ? WHERE id = ?`, sentinel, surface.ID); err != nil {
		t.Fatalf("set runtime revision sentinel: %v", err)
	}

	unchanged, err := store.EnsureRuntimeSurface(ctx, input)
	if err != nil {
		t.Fatalf("ensure unchanged runtime surface: %v", err)
	}
	if unchanged.UpdatedAt != sentinel {
		t.Fatalf("unchanged runtime surface revision = %q, want %q", unchanged.UpdatedAt, sentinel)
	}

	input.Label = "renamed"
	changed, err := store.EnsureRuntimeSurface(ctx, input)
	if err != nil {
		t.Fatalf("ensure changed runtime surface: %v", err)
	}
	if changed.UpdatedAt == sentinel || changed.Label != "renamed" {
		t.Fatalf("changed runtime surface was not revised: %#v", changed)
	}
}

func TestTargetAndCredentialMutationsWaitForTransferRecovery(t *testing.T) {
	database := openTargetTestDB(t)
	ctx := t.Context()
	store := NewStore(database)
	keyID := insertTargetTestSSHKey(t, database, "recovery")
	target, profile := createTargetTestSSHProfile(t, ctx, store, keyID, "recovery-host", "admin", "10.0.0.10", 22)
	surface, err := store.EnsureRuntimeSurface(ctx, EnsureRuntimeSurfaceInput{
		ConnectorKind: sshconnector.Kind, TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: RuntimeCapabilityFileTransfer, Label: profile.Label,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO file_transfers (
			runtime_id, direction, source, status, remote_path, remote_staging_ref, created_at, updated_at
		) VALUES (?, 'upload', 'ui', 'failed', '/remote', 'opaque-recovery-ref', datetime('now'), datetime('now'))`, surface.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateTarget(ctx, UpdateTargetInput{
		ID: target.ID, ProjectID: target.ProjectID, Name: target.Name, Config: target.Config,
	}); !errors.Is(err, ErrRemoteCleanupPending) {
		t.Fatalf("target update error = %v", err)
	}
	if _, err := store.UpdateCredentialProfile(ctx, UpdateCredentialProfileInput{
		TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: profile.ConnectorKind,
		Kind: profile.Kind, Label: profile.Label, Public: profile.Public, RiskLabel: profile.RiskLabel,
	}); !errors.Is(err, ErrRemoteCleanupPending) {
		t.Fatalf("credential update error = %v", err)
	}
	if err := store.SetCredentialProfileEncryptedSecret(ctx, target.ID, profile.ID, "changed"); !errors.Is(err, ErrRemoteCleanupPending) {
		t.Fatalf("secret update error = %v", err)
	}
	if err := store.DeleteCredentialProfile(ctx, target.ID, profile.ID); !errors.Is(err, ErrRemoteCleanupPending) {
		t.Fatalf("credential delete error = %v", err)
	}
	if err := store.DeleteTarget(ctx, target.ID); !errors.Is(err, ErrRemoteCleanupPending) {
		t.Fatalf("target delete error = %v", err)
	}
}

func TestStoreTargetProfileByRuntimeIDRejectsArchivedProfile(t *testing.T) {
	database := openTargetTestDB(t)
	ctx := context.Background()
	keyID := insertTargetTestSSHKey(t, database, "main")
	store := NewStore(database)
	target, profile := createTargetTestSSHProfile(t, ctx, store, keyID, "core-1", "admin", "10.0.0.10", 2222)
	surface, err := store.EnsureRuntimeSurface(ctx, EnsureRuntimeSurfaceInput{
		ConnectorKind:  sshconnector.Kind,
		TargetID:       target.ID,
		ProfileID:      profile.ID,
		CapabilityKind: RuntimeCapabilityLiveConsole,
		Label:          profile.Label,
	})
	if err != nil {
		t.Fatalf("ensure runtime surface: %v", err)
	}

	if err := store.DeleteCredentialProfile(ctx, target.ID, profile.ID); err != nil {
		t.Fatalf("delete credential profile: %v", err)
	}

	_, _, _, err = store.TargetProfileByRuntimeID(ctx, surface.ID)
	if !errors.Is(err, ErrRuntimeSurfaceNotFound) {
		t.Fatalf("archived profile should not resolve runtime surface, got %v", err)
	}
}

func TestStoreAllowsSecondSSHCredentialProfile(t *testing.T) {
	database := openTargetTestDB(t)
	ctx := context.Background()
	keyID := insertTargetTestSSHKey(t, database, "main")
	store := NewStore(database)
	target, _ := createTargetTestSSHProfile(t, ctx, store, keyID, "core-1", "admin", "10.0.0.10", 2222)

	profile, err := store.CreateCredentialProfile(ctx, CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       sshconnector.Kind,
		Kind:                "private_key",
		Label:               "readonly",
		EncryptedSecretJSON: "{}",
		Public: map[string]any{
			"username":   "readonly",
			"ssh_key_id": keyID,
		},
	})
	if err != nil {
		t.Fatalf("second SSH credential profile should be allowed: %v", err)
	}
	if profile.TargetID != target.ID || profile.Label != "readonly" {
		t.Fatalf("unexpected second profile: %#v", profile)
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
		INSERT INTO connector_credential_resources (
			connector_kind, resource_kind, name, resource_type, public_data, encrypted_secret, fingerprint, created_at, updated_at
		)
		VALUES ('ssh', 'private_key', ?, 'ed25519', 'ssh-ed25519 AAAATEST aipermission-test', 'encrypted', 'SHA256:test', ?, ?)`,
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

func createTargetTestSSHProfile(t *testing.T, ctx context.Context, store *Store, sshKeyID int64, name string, username string, host string, port int) (Target, CredentialProfile) {
	t.Helper()
	target, err := store.CreateTarget(ctx, CreateTargetInput{
		ConnectorKind: sshconnector.Kind,
		Name:          name,
		Config: map[string]any{
			"host":                        host,
			"port":                        port,
			"description":                 "NAS gateway",
			"startup_input_after_connect": "q",
			"force_shell_command":         "bash -l",
		},
	})
	if err != nil {
		t.Fatalf("create ssh target: %v", err)
	}
	profile, err := store.CreateCredentialProfile(ctx, CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       sshconnector.Kind,
		Kind:                "private_key",
		Label:               username,
		EncryptedSecretJSON: "{}",
		Public: map[string]any{
			"username":    username,
			"ssh_key_id":  sshKeyID,
			"key_name":    "main",
			"key_type":    "ed25519",
			"fingerprint": "SHA256:test",
		},
	})
	if err != nil {
		t.Fatalf("create ssh profile: %v", err)
	}
	return target, profile
}
