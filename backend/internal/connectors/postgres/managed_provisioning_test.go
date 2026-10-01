package postgresconnector

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestManagedProvisioningRejectsInvalidPlanBeforeSecretOrRemoteAccess(t *testing.T) {
	for _, input := range []map[string]any{
		{}, {"role_name": "unsafe;role"}, {"role_name": "reader", "preset": "admin"},
		{"role_name": "reader", "scope": "not JSON"},
	} {
		runtime, _, store, secrets := managedLifecycleFixture(t)
		result, err := New().ProvisionCredentialProfile(t.Context(), runtime, input)
		if err == nil || result.Kind != "" || store.reads != 0 || secrets.reads != 0 {
			t.Fatalf("invalid provision plan was dispatched: %v", err)
		}
	}
}

func TestManagedProvisioningRequiresCurrentJournalAndRegistersGeneratedPassword(t *testing.T) {
	for _, mode := range []string{"connector", "journal", "authority", "explicit write preset", "default read preset", "unsupported column write scope"} {
		t.Run(mode, func(t *testing.T) {
			runtime, _, store, secrets := managedLifecycleFixture(t)
			input := map[string]any{"role_name": "reader", "profile_label": "My reader"}
			switch mode {
			case "connector":
				runtime.Target.ConnectorKind = "other"
			case "journal":
				runtime.Capabilities = nil
			case "authority":
				runtime.Profile.TargetID++
			case "explicit write preset":
				input["preset"] = "read_write"
			case "unsupported column write scope":
				input["preset"] = "read_write"
				input["scope"] = map[string]any{"schemas": []any{map[string]any{"schema": "public", "tables": []any{
					map[string]any{"table": "users", "columns": []any{"id"}},
				}}}}
			}
			result, err := New().ProvisionCredentialProfile(t.Context(), runtime, input)
			if err == nil || result.Kind != "" || store.reads != 0 {
				t.Fatalf("failed predispatch exposed credentials or journal mutation: %v", err)
			}
			generated := mode == "explicit write preset" || mode == "default read preset" || mode == "unsupported column write scope"
			if secrets.registered != generated {
				t.Fatal("generated password was not registered for error redaction before possible remote dispatch")
			}
		})
	}
}

func TestManagedIdentityIsDeclaredOnlyAsPublicConnectorEvidence(t *testing.T) {
	schemas := New().CredentialSchemas()
	for _, field := range schemas[0].Schema.Fields {
		if field.Name == "managed_identity" {
			if field.Type != connectors.FieldJSON || field.Secret || field.Required {
				t.Fatal("managed identity schema grants secret or ordinary-create authority")
			}
			return
		}
	}
	t.Fatal("managed identity is missing from connector provisioning schema")
}
