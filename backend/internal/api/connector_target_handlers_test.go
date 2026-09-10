package api

import (
	"context"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestMergeConnectorCredentialSecretsPreservesUnchangedFields(t *testing.T) {
	secretVault, err := vault.New("credential-merge-test-secret")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	previous, err := recordcrypto.EncryptJSON(secretVault, "credential-test-workspace", recordcrypto.ConnectorCredentialProfile, 1, map[string]any{
		"primary_username":   "operator@example.com",
		"primary_password":   "old-primary-password",
		"secondary_username": "service@example.com",
		"secondary_password": "old-secondary-password",
	})
	if err != nil {
		t.Fatalf("encrypt previous secret: %v", err)
	}
	profile := &connectors.CredentialProfileView{ID: 1}
	merged, err := mergeConnectorCredentialSecrets(&databaseRuntime{vault: secretVault, workspaceUUID: "credential-test-workspace"}, profile, previous, map[string]any{
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
	previous, err := recordcrypto.EncryptJSON(secretVault, "credential-test-workspace", recordcrypto.ConnectorCredentialProfile, 1, map[string]any{"username": "support", "password": "secret"})
	if err != nil {
		t.Fatalf("encrypt previous secret: %v", err)
	}
	profile := &connectors.CredentialProfileView{ID: 1}
	merged, err := mergeConnectorCredentialSecrets(&databaseRuntime{vault: secretVault, workspaceUUID: "credential-test-workspace"}, profile, previous, nil)
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
	previous, err := recordcrypto.EncryptJSON(secretVault, "credential-test-workspace", recordcrypto.ConnectorCredentialProfile, 1, map[string]any{"password": "secret", "session_token": "temporary"})
	if err != nil {
		t.Fatalf("encrypt previous secret: %v", err)
	}
	profile := &connectors.CredentialProfileView{ID: 1}
	merged, err := mergeConnectorCredentialSecrets(&databaseRuntime{vault: secretVault, workspaceUUID: "credential-test-workspace"}, profile, previous, map[string]any{"session_token": nil})
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
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID)
	err := fixture.server.validateConnectorTransportConfig(context.Background(), store, target.ProjectID, map[string]any{
		"connection_mode":      "over_fixture",
		"transport_target_ref": connectors.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID),
	})
	if err == nil || !strings.Contains(err.Error(), "does not expose reviewed TCP transport") {
		t.Fatalf("transport validation error = %v", err)
	}
}
