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

func TestTargetProfileRefRoundTrip(t *testing.T) {
	ref := TargetProfileRef("ssh", 42, 7)
	if ref != "ssh:42:7" {
		t.Fatalf("ref = %q", ref)
	}
	targetID, profileID, ok := ParseTargetProfileRef("ssh", ref)
	if !ok || targetID != 42 || profileID != 7 {
		t.Fatalf("parse = %d %d ok=%v", targetID, profileID, ok)
	}
}

func TestParseTargetProfileRefRejectsInvalidRefs(t *testing.T) {
	for _, ref := range []string{"", "server:1:2", "ssh:", "ssh:0:1", "ssh:1:0", "ssh:not-number", "ssh:1"} {
		if _, _, ok := ParseTargetProfileRef("ssh", ref); ok {
			t.Fatalf("expected %q to be rejected", ref)
		}
	}
}

func TestResolverMapsSSHConnectorProfileToConnectorViews(t *testing.T) {
	database := openTargetTestDB(t)
	ctx := context.Background()
	keyID := insertTargetTestSSHKey(t, database, "main")
	store := NewStore(database)
	target, profile := createTargetTestSSHProfile(t, ctx, store, keyID, "core-1", "admin", "10.0.0.10", 2222)
	targetRef := TargetProfileRef("ssh", target.ID, profile.ID)

	resolved, err := NewResolver(database).ResolveActionTarget(ctx, targetRef)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}

	targetID, profileID, ok := ParseTargetProfileRef("ssh", targetRef)
	if !ok {
		t.Fatalf("invalid ssh target ref: %q", targetRef)
	}
	if resolved.Target.ID != targetID || resolved.Target.Ref != targetRef {
		t.Fatalf("unexpected target identity: %#v", resolved.Target)
	}
	if resolved.Target.ConnectorKind != sshconnector.Kind {
		t.Fatalf("connector kind = %q", resolved.Target.ConnectorKind)
	}
	if resolved.Target.Config["host"] != "10.0.0.10" || resolved.Target.Config["port"] != float64(2222) {
		t.Fatalf("unexpected target config: %#v", resolved.Target.Config)
	}
	if resolved.Target.Config["startup_input_after_connect"] != "q" {
		t.Fatalf("startup input missing: %#v", resolved.Target.Config)
	}

	if resolved.Profile.ID != profileID || resolved.Profile.TargetID != targetID {
		t.Fatalf("unexpected profile identity: %#v", resolved.Profile)
	}
	if resolved.Profile.ConnectorKind != sshconnector.Kind || resolved.Profile.Kind != "private_key" {
		t.Fatalf("unexpected profile kind: %#v", resolved.Profile)
	}
	if resolved.Profile.Public["username"] != "admin" {
		t.Fatalf("username public metadata missing: %#v", resolved.Profile.Public)
	}
	if resolved.Profile.Public["ssh_key_id"].(float64) != float64(keyID) {
		t.Fatalf("ssh_key_id public metadata missing: %#v", resolved.Profile.Public)
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

	for _, ref := range []string{"ssh:999:1", "postgres:1", "ssh:bad"} {
		_, err := resolver.ResolveActionTarget(context.Background(), ref)
		if !errors.Is(err, actions.ErrTargetNotFound) {
			t.Fatalf("ResolveActionTarget(%q) error = %v", ref, err)
		}
	}
}

func TestStoreTargetProfileByProfileIDUsesCredentialProfile(t *testing.T) {
	database := openTargetTestDB(t)
	ctx := context.Background()
	keyID := insertTargetTestSSHKey(t, database, "main")
	store := NewStore(database)
	target, profile := createTargetTestSSHProfile(t, ctx, store, keyID, "core-1", "admin", "10.0.0.10", 2222)

	gotTarget, gotProfile, err := store.TargetProfileByProfileID(ctx, profile.ID)
	if err != nil {
		t.Fatalf("target profile by profile id: %v", err)
	}
	if gotTarget.ID != target.ID || gotProfile.ID != profile.ID || gotProfile.TargetID != target.ID {
		t.Fatalf("unexpected target/profile: target=%#v profile=%#v", gotTarget, gotProfile)
	}
	if gotProfile.Public["username"] != "admin" || gotProfile.Public["ssh_key_id"].(float64) != float64(keyID) {
		t.Fatalf("unexpected credential metadata: %#v", gotProfile.Public)
	}
}

func TestStoreTargetProfileByProfileIDRejectsArchivedProfile(t *testing.T) {
	database := openTargetTestDB(t)
	ctx := context.Background()
	keyID := insertTargetTestSSHKey(t, database, "main")
	store := NewStore(database)
	target, profile := createTargetTestSSHProfile(t, ctx, store, keyID, "core-1", "admin", "10.0.0.10", 2222)

	if err := store.DeleteCredentialProfile(ctx, target.ID, profile.ID); err != nil {
		t.Fatalf("delete credential profile: %v", err)
	}

	_, _, err := store.TargetProfileByProfileID(ctx, profile.ID)
	if !errors.Is(err, ErrTargetProfileNotFound) {
		t.Fatalf("archived profile should not resolve target profile, got %v", err)
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
