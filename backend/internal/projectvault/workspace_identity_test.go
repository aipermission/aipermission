package projectvault

import (
	"path/filepath"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestResolveGatewaySecretPreservesWorkspaceBinding(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "identity.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	secret, err := ResolveGatewaySecret(t.Context(), database, " bootstrap-secret ")
	if err != nil || secret != "bootstrap-secret" {
		t.Fatalf("bootstrap secret = %q, %v", secret, err)
	}
	secret, err = ResolveGatewaySecret(t.Context(), database, "different-secret")
	if err != nil || secret != "bootstrap-secret" {
		t.Fatalf("stored secret = %q, %v", secret, err)
	}
	if _, err := database.ExecContext(t.Context(), `DELETE FROM settings WHERE key = ?`, gatewaySecretSetting); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `INSERT INTO settings (key, value, updated_at) VALUES ('encrypted_record_envelope_version', '1', datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveGatewaySecret(t.Context(), database, "different-secret"); err == nil {
		t.Fatal("envelope-bound database accepted a fallback secret")
	}
}

func TestResolveGatewaySecretRejectsMissingInputs(t *testing.T) {
	if _, err := ResolveGatewaySecret(t.Context(), nil, "secret"); err == nil {
		t.Fatal("nil database unexpectedly resolved a secret")
	}
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "identity.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := ResolveGatewaySecret(t.Context(), database, ""); err == nil {
		t.Fatal("empty fallback unexpectedly resolved a secret")
	}
	if _, err := database.ExecContext(t.Context(), `DROP TABLE settings`); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveGatewaySecret(t.Context(), database, "secret"); err == nil {
		t.Fatalf("storage failure = %v", err)
	}
}
