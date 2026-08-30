package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestIntConfigValueBoundsParsedInputToNativeInt(t *testing.T) {
	config := map[string]any{
		"from_string": "22",
		"from_json":   json.Number("5432"),
		"overflow":    "9223372036854775808",
		"zero":        0,
	}

	if got := intConfigValue(config, "from_string", 99); got != 22 {
		t.Fatalf("from string = %d, want 22", got)
	}
	if got := intConfigValue(config, "from_json", 99); got != 5432 {
		t.Fatalf("from json = %d, want 5432", got)
	}
	if got := intConfigValue(config, "overflow", 99); got != 99 {
		t.Fatalf("overflow = %d, want fallback 99", got)
	}
	if got := intConfigValue(config, "zero", 99); got != 99 {
		t.Fatalf("zero = %d, want fallback 99", got)
	}
}

func TestMergeConnectorCredentialSecretsPreservesUnchangedFields(t *testing.T) {
	secretVault, err := vault.New("credential-merge-test-secret")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	previous, err := secretVault.EncryptJSON(map[string]any{
		"primary_username":   "operator@example.com",
		"primary_password":   "old-primary-password",
		"secondary_username": "service@example.com",
		"secondary_password": "old-secondary-password",
	})
	if err != nil {
		t.Fatalf("encrypt previous secret: %v", err)
	}
	merged, err := mergeConnectorCredentialSecrets(&databaseRuntime{vault: secretVault}, previous, map[string]any{
		"secondary_password": "new-secondary-password",
	})
	if err != nil {
		t.Fatalf("merge secrets: %v", err)
	}
	if merged["primary_username"] != "operator@example.com" || merged["primary_password"] != "old-primary-password" || merged["secondary_username"] != "service@example.com" {
		t.Fatalf("unchanged secrets were not preserved: %#v", merged)
	}
	if merged["secondary_password"] != "new-secondary-password" {
		t.Fatalf("updated secret = %#v", merged["secondary_password"])
	}
}

func TestMergeConnectorCredentialSecretsLoadsPreviousFieldsForMetadataOnlyEdit(t *testing.T) {
	secretVault, err := vault.New("credential-metadata-edit-secret")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	previous, err := secretVault.EncryptJSON(map[string]any{"username": "support", "password": "secret"})
	if err != nil {
		t.Fatalf("encrypt previous secret: %v", err)
	}
	merged, err := mergeConnectorCredentialSecrets(&databaseRuntime{vault: secretVault}, previous, nil)
	if err != nil {
		t.Fatalf("merge secrets: %v", err)
	}
	if merged["username"] != "support" || merged["password"] != "secret" {
		t.Fatalf("previous secrets were not loaded for validation: %#v", merged)
	}
}

func TestMergeConnectorCredentialSecretsAllowsExplicitRemoval(t *testing.T) {
	secretVault, err := vault.New("credential-remove-test-secret")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	previous, err := secretVault.EncryptJSON(map[string]any{"password": "secret", "session_token": "temporary"})
	if err != nil {
		t.Fatalf("encrypt previous secret: %v", err)
	}
	merged, err := mergeConnectorCredentialSecrets(&databaseRuntime{vault: secretVault}, previous, map[string]any{"session_token": nil})
	if err != nil {
		t.Fatalf("merge secrets: %v", err)
	}
	if merged["password"] != "secret" {
		t.Fatalf("password was not preserved: %#v", merged)
	}
	if _, exists := merged["session_token"]; exists {
		t.Fatalf("session token was not removed: %#v", merged)
	}
}

func TestTransportConfigRejectsTargetsWithoutReviewedTCPAdapter(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	err := fixture.server.validateConnectorTransportConfig(context.Background(), store, target.ProjectID, map[string]any{
		"connection_mode":      "over_ssh",
		"transport_target_ref": connectortargets.ConnectorTargetRef(target.ConnectorKind, target.ID, profile.ID),
	})
	if err == nil || !strings.Contains(err.Error(), "does not expose reviewed TCP transport") {
		t.Fatalf("transport validation error = %v", err)
	}
}

func TestConnectorTargetConfigRunsConnectorSemanticValidator(t *testing.T) {
	if err := validateConnectorTargetConfig(localActionTestConnector{}, map[string]any{"semantic_error": true}); err == nil || !strings.Contains(err.Error(), "semantic validation fixture") {
		t.Fatalf("expected semantic target validation error, got %v", err)
	}
}
