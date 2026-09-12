package connectormanagement

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestRuntimeCredentialPreparationCopiesPublicFieldsWithoutCanonicalizer(t *testing.T) {
	input := map[string]any{"host": "database.internal"}
	ports := RuntimeCredentialPreparation(CredentialStorage{}, nil)
	result, err := ports.Canonicalize(t.Context(), "fixture", "password", input)
	if err != nil {
		t.Fatal(err)
	}
	result["host"] = "changed"
	if input["host"] != "database.internal" {
		t.Fatal("canonicalization fallback aliased caller input")
	}
}

func TestRuntimeCredentialPortsBuildsScopedSecretAccessor(t *testing.T) {
	ports := RuntimeCredentialPorts(CredentialStorage{}, nil, nil, nil)
	boundary := actionresult.NewCredentialBoundary(nil)
	runtimeContext := ports.RuntimeContext(
		connectortargets.Target{ID: 4, ConnectorKind: "fixture", Name: "target", Config: map[string]any{"mode": "direct"}},
		connectortargets.CredentialProfile{ID: 7, TargetID: 4, Label: "main"},
		map[string]any{"password": "secret-value"}, boundary,
	)
	value, err := runtimeContext.Secrets.GetSecret(context.Background(), "password")
	if err != nil || value != "secret-value" {
		t.Fatalf("secret value=%q error=%v", value, err)
	}
	if runtimeContext.Target.Ref != connectors.FormatTargetRef("fixture", 4, 7) {
		t.Fatalf("target ref=%q", runtimeContext.Target.Ref)
	}
	if boundary.Redact("prefix secret-value suffix") == "prefix secret-value suffix" {
		t.Fatal("credential boundary did not register accessed secret")
	}
}

func TestRuntimeCredentialPortsWireCryptoCanonicalizationAndRedaction(t *testing.T) {
	secretVault, err := vault.New("ManagementRuntimePortsPassword123")
	if err != nil {
		t.Fatal(err)
	}
	storage := CredentialStorage{Vault: secretVault, WorkspaceID: "management-runtime-workspace"}
	preparation := RuntimeCredentialPreparation(storage, func(kind string) CredentialCanonicalizer {
		if kind != "fixture" {
			t.Fatalf("connector kind=%q", kind)
		}
		return func(_ context.Context, connectorKind, credentialKind string, public map[string]any) (map[string]any, error) {
			return map[string]any{
				"connector_kind": connectorKind, "credential_kind": credentialKind, "host": public["host"],
			}, nil
		}
	})
	canonical, err := preparation.Canonicalize(
		t.Context(), "fixture", "password", map[string]any{"host": "database.internal"},
	)
	if err != nil || canonical["connector_kind"] != "fixture" || canonical["credential_kind"] != "password" {
		t.Fatalf("canonical=%#v error=%v", canonical, err)
	}
	encrypted, err := preparation.Encrypt(t.Context(), 7, map[string]any{"password": "runtime-secret"})
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := preparation.Decrypt(t.Context(), 7, encrypted)
	if err != nil || decrypted["password"] != "runtime-secret" {
		t.Fatalf("decrypted=%#v error=%v", decrypted, err)
	}

	redactCalls := 0
	ports := RuntimeCredentialPorts(
		storage,
		func(kind string) connectors.RuntimeCapabilityResolver {
			if kind != "fixture" {
				t.Fatalf("capability kind=%q", kind)
			}
			return nil
		},
		func(_ context.Context, result connectors.ActionResult, _ CredentialBoundary) (connectors.ActionResult, error) {
			redactCalls++
			return result, nil
		},
		func(_ context.Context, value string) string {
			redactCalls++
			return "redacted:" + value
		},
	)
	decrypted, err = ports.DecryptSecret(t.Context(), 7, encrypted)
	if err != nil || decrypted["password"] != "runtime-secret" {
		t.Fatalf("runtime decrypted=%#v error=%v", decrypted, err)
	}
	boundary := actionresult.NewCredentialBoundary(nil)
	runtimeContext := ports.RuntimeContext(
		connectortargets.Target{ID: 4, ConnectorKind: "fixture"},
		connectortargets.CredentialProfile{ID: 7, TargetID: 4},
		decrypted, boundary,
	)
	registrar, ok := runtimeContext.Secrets.(connectors.SensitiveValueRegistrar)
	if !ok {
		t.Fatal("runtime secret accessor does not expose sensitive-value registration")
	}
	registrar.RegisterSensitiveValue("derived-secret")
	if boundary.Redact("derived-secret") == "derived-secret" {
		t.Fatal("derived secret was not registered")
	}
	if _, err := runtimeContext.Secrets.GetSecret(t.Context(), "missing"); err == nil {
		t.Fatal("missing secret was accepted")
	}
	if err := runtimeContext.Events.Emit(t.Context(), connectors.ActionEvent{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ports.RedactResult(t.Context(), connectors.ActionResult{}, boundary); err != nil {
		t.Fatal(err)
	}
	if got := ports.RedactText(t.Context(), "value"); got != "redacted:value" || redactCalls != 2 {
		t.Fatalf("redacted text=%q calls=%d", got, redactCalls)
	}
}
