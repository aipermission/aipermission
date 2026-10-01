package connectorruntime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectorcredentials"
	"github.com/aipermission/aipermission/backend/internal/connectorcredentials/profilesecrets"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestTransferRuntimeUsesProfileBoundSecretCodec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.aipdb")
	database, _ := openScopedResourceFixture(t, path)
	secretVault, err := vault.New("scoped-resources-fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{ConnectorKind: "fixture", Name: "fixture-target"})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "fixture", Kind: "password", Label: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	surface, err := store.EnsureRuntimeSurface(t.Context(), connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: "fixture", TargetID: target.ID, ProfileID: profile.ID, CapabilityKind: "file_transfer",
	})
	if err != nil {
		t.Fatal(err)
	}
	accessorCalls := 0
	dependencies := Dependencies{Database: database, Vault: secretVault, WorkspaceID: "fixture-workspace",
		SecretAccessor: func(secrets map[string]any) connectors.SecretAccessor {
			accessorCalls++
			return connectorcredentials.Secrets(secrets, actionresult.NewCredentialBoundary(secrets))
		},
	}
	codec := profilesecrets.NewProfileSecretCodec(secretVault, dependencies.WorkspaceID)
	encrypted, err := codec.Encrypt(t.Context(), profile.ID, map[string]any{"password": "fixture-value"})
	if err != nil {
		t.Fatal(err)
	}
	profile, err = store.UpdateCredentialProfile(t.Context(), connectortargets.UpdateCredentialProfileInput{
		TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: "fixture", Kind: profile.Kind, Label: profile.Label, EncryptedSecretJSON: &encrypted,
		ExpectedSecretRevision: &profile.SecretRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(kind string, input Dependencies, capability string) (connectors.RuntimeContext, error) {
		result, gotSurface, err := NewScope(kind, input).TransferRuntime().ResolveRuntimeContext(t.Context(), surface.ID, capability)
		if err == nil && gotSurface.ID != surface.ID {
			t.Fatal("runtime surface identity changed")
		}
		return result, err
	}
	result, err := resolve("fixture", dependencies, "file_transfer")
	if err != nil {
		t.Fatal(err)
	}
	if result.Profile.ID != profile.ID || result.Target.ID != target.ID || result.Target.Ref != connectors.FormatTargetRef("fixture", target.ID, profile.ID) {
		t.Fatal("runtime resolution lost target/profile authority")
	}
	if value, err := result.Secrets.GetSecret(t.Context(), "password"); err != nil || value != "fixture-value" || accessorCalls != 1 {
		t.Fatalf("runtime codec did not preserve secret: %q %v", value, err)
	}
	wrongWorkspace := dependencies
	wrongWorkspace.WorkspaceID = "another-workspace"
	missingVault := dependencies
	missingVault.Vault = nil
	for _, scenario := range []struct {
		name, kind, capability string
		input                  Dependencies
		expected               error
	}{
		{"workspace", "fixture", "file_transfer", wrongWorkspace, nil},
		{"vault", "fixture", "file_transfer", missingVault, ErrInvalidRuntime},
		{"connector", "foreign", "file_transfer", dependencies, connectortargets.ErrRuntimeSurfaceNotFound},
		{"capability", "fixture", "live_console", dependencies, connectortargets.ErrRuntimeSurfaceNotFound},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			before := accessorCalls
			got, err := resolve(scenario.kind, scenario.input, scenario.capability)
			if err == nil || (scenario.expected != nil && !errors.Is(err, scenario.expected)) || got.Secrets != nil || accessorCalls != before {
				t.Fatalf("invalid authority reached secret accessor: %#v %v", got, err)
			}
		})
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	before := accessorCalls
	if got, _, err := NewScope("fixture", dependencies).TransferRuntime().ResolveRuntimeContext(canceled, surface.ID, "file_transfer"); !errors.Is(err, context.Canceled) || got.Secrets != nil || accessorCalls != before {
		t.Fatalf("canceled runtime reached secrets: %#v %v", got, err)
	}
	wrongProfile, err := codec.Encrypt(t.Context(), profile.ID+1, map[string]any{"password": "another-profile"})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		encrypted string
		rejected  bool
	}{{wrongProfile, true}, {encrypted, false}} {
		profile, err = store.UpdateCredentialProfile(t.Context(), connectortargets.UpdateCredentialProfileInput{
			TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: "fixture", Kind: profile.Kind, Label: profile.Label, EncryptedSecretJSON: &scenario.encrypted,
			ExpectedSecretRevision: &profile.SecretRevision,
		})
		if err != nil {
			t.Fatal(err)
		}
		before := accessorCalls
		got, err := resolve("fixture", dependencies, "file_transfer")
		if scenario.rejected {
			if err == nil || got.Secrets != nil || accessorCalls != before {
				t.Fatalf("another profile's ciphertext reached accessor: %#v %v", got, err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, _ := openScopedResourceFixture(t, path)
	dependencies.Database = reopened
	got, err := resolve("fixture", dependencies, "file_transfer")
	if err != nil {
		t.Fatal(err)
	}
	if value, err := got.Secrets.GetSecret(t.Context(), "password"); err != nil || value != "fixture-value" {
		t.Fatalf("persisted profile binding changed on reopen: %q %v", value, err)
	}
}
