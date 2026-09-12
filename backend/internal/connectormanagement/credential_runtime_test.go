package connectormanagement

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type managedCleanupTestConnector struct {
	managementTestConnector
	adminProfileID int64
}

func (c managedCleanupTestConnector) ProvisionedCredentialAdminProfileID(
	profile connectors.CredentialProfileView,
) (int64, bool, error) {
	return c.adminProfileID, profile.Label == "managed-cleanup", nil
}

func TestManagedCredentialCleanupUsesAdminIdentityAndRedactsResult(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	store := connectortargets.NewStore(fixture.database)
	if err := store.SetCredentialProfileEncryptedSecret(
		t.Context(), fixture.target.ID, fixture.profile.ID, "encrypted-admin-secret",
	); err != nil {
		t.Fatal(err)
	}
	managed, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: fixture.target.ID, ConnectorKind: fixture.target.ConnectorKind,
		Kind: "operator", Label: "managed-cleanup",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetCredentialProfileEncryptedSecret(
		t.Context(), fixture.target.ID, managed.ID, "encrypted-managed-secret",
	); err != nil {
		t.Fatal(err)
	}
	managed, err = store.GetCredentialProfile(t.Context(), fixture.target.ID, managed.ID)
	if err != nil {
		t.Fatal(err)
	}
	registry := connectors.NewRegistry()
	if err := registry.Register(managedCleanupTestConnector{
		adminProfileID: fixture.profile.ID,
	}); err != nil {
		t.Fatal(err)
	}
	decryptedProfileIDs := []int64{}
	runtime := managementCredentialRuntimePorts()
	runtime.DecryptSecret = func(_ context.Context, profileID int64, encrypted string) (map[string]any, error) {
		decryptedProfileIDs = append(decryptedProfileIDs, profileID)
		return map[string]any{"password": encrypted}, nil
	}
	redacted := 0
	runtime.RedactResult = func(_ context.Context, result connectors.ActionResult, boundary CredentialBoundary) (connectors.ActionResult, error) {
		redacted++
		result.Output = map[string]any{"summary": boundary.Redact("encrypted-admin-secret")}
		return result, nil
	}
	outcome, err := CleanupProvisionedCredentialProfileIfNeeded(
		t.Context(),
		ManagedCredentialCleanupScope{Database: fixture.database, Registry: registry, Runtime: runtime},
		fixture.target,
		managed,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Required || outcome.Status != string(connectors.ResultCompleted) || redacted != 1 {
		t.Fatalf("outcome=%#v redacted=%d", outcome, redacted)
	}
	if len(decryptedProfileIDs) != 2 || decryptedProfileIDs[0] != fixture.profile.ID || decryptedProfileIDs[1] != managed.ID {
		t.Fatalf("decrypted profile ids=%v", decryptedProfileIDs)
	}
	output, ok := outcome.Output.(map[string]any)
	if !ok {
		t.Fatalf("cleanup output=%#v", outcome.Output)
	}
	if output["summary"] == "encrypted-admin-secret" {
		t.Fatal("cleanup output exposed an admin credential")
	}
}
