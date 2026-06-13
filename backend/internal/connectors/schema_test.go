package connectors

import (
	"strings"
	"testing"
)

func TestValidateNonSecretSchemaRejectsTargetSecrets(t *testing.T) {
	err := ValidateNonSecretSchema(Schema{Fields: []Field{
		{Name: "host", Type: FieldString, Required: true},
		{Name: "password", Type: FieldSecret, Secret: true},
	}}, "target")
	if err == nil || !strings.Contains(err.Error(), "credential profiles") {
		t.Fatalf("expected target secret schema to be rejected, got %v", err)
	}
}

func TestValidateNonSecretSchemaAllowsPublicTargetFields(t *testing.T) {
	if err := ValidateNonSecretSchema(Schema{Fields: []Field{
		{Name: "host", Type: FieldString, Required: true},
		{Name: "port", Type: FieldNumber, Required: true},
	}}, "target"); err != nil {
		t.Fatalf("expected public target schema to pass, got %v", err)
	}
}

func TestValidateCredentialSchemaDefinitionRejectsSecretTypeWithoutSecretFlag(t *testing.T) {
	err := ValidateCredentialSchemaDefinition(CredentialSchema{
		Kind: "api_key",
		Schema: Schema{Fields: []Field{
			{Name: "token", Type: FieldSecret, Required: true},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "secret=true") {
		t.Fatalf("expected secret=true schema error, got %v", err)
	}
}

func TestValidateCredentialSchemaDefinitionRejectsSecretDefaults(t *testing.T) {
	err := ValidateCredentialSchemaDefinition(CredentialSchema{
		Kind: "api_key",
		Schema: Schema{Fields: []Field{
			{Name: "token", Type: FieldSecret, Secret: true, Required: true, Default: "leaked-token"},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "must not declare a default") {
		t.Fatalf("expected secret default schema error, got %v", err)
	}
}

func TestValidateCredentialSchemaValuesTreatsSecretTypesAsSecret(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "username", Type: FieldString, Required: true},
		{Name: "password", Type: FieldSecret, Required: true},
	}}
	err := ValidateCredentialSchemaValues(schema, map[string]any{
		"username": "app",
		"password": "leak",
	}, nil, true)
	if err == nil || !strings.Contains(err.Error(), "unsupported public credential field") {
		t.Fatalf("expected public secret field to be rejected, got %v", err)
	}
	if err := ValidateCredentialSchemaValues(schema, map[string]any{
		"username": "app",
	}, map[string]any{"password": "safe"}, true); err != nil {
		t.Fatalf("expected secret field in secret map to pass, got %v", err)
	}
}

func TestValidateActionDefinitionsRejectsInvalidContracts(t *testing.T) {
	if err := ValidateActionDefinitions([]ActionDefinition{
		{Name: "run", Risk: RiskRead},
		{Name: "run", Risk: RiskRead},
	}, "test"); err == nil || !strings.Contains(err.Error(), "duplicate action") {
		t.Fatalf("expected duplicate action error, got %v", err)
	}
	if err := ValidateActionDefinitions([]ActionDefinition{{
		Name: "run",
		Risk: RiskRead,
		InputSchema: Schema{Fields: []Field{
			{Name: "password", Type: FieldSecret, Secret: true},
		}},
	}}, "test"); err == nil || !strings.Contains(err.Error(), "must not be secret") {
		t.Fatalf("expected secret action input error, got %v", err)
	}
	if err := ValidateActionDefinitions([]ActionDefinition{{
		Name: "run",
		Risk: RiskLevel("mystery"),
	}}, "test"); err == nil || !strings.Contains(err.Error(), "unsupported risk") {
		t.Fatalf("expected unsupported risk error, got %v", err)
	}
}
