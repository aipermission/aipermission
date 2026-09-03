package connectors

import (
	"encoding/json"
	"math"
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
		{Name: "port", Type: FieldInteger, Required: true},
	}}, "target"); err != nil {
		t.Fatalf("expected public target schema to pass, got %v", err)
	}
}

func TestNormalizeSchemaValuesCanonicalizesNumericStrings(t *testing.T) {
	normalized, err := NormalizeSchemaValues(Schema{Fields: []Field{
		{Name: "host", Type: FieldString, Required: true},
		{Name: "port", Type: FieldNumber, Required: true},
	}}, map[string]any{
		"host": "localhost",
		"port": "5432",
	})
	if err != nil {
		t.Fatalf("normalize schema values: %v", err)
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("marshal normalized values: %v", err)
	}
	if string(payload) != `{"host":"localhost","port":5432}` {
		t.Fatalf("normalized payload = %s", payload)
	}
}

func TestNormalizeSchemaValuesCanonicalizesExactIntegers(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "limit", Type: FieldInteger, Required: true}}}
	for _, value := range []any{42, int64(42), float64(42), json.Number("42"), "42"} {
		normalized, err := NormalizeSchemaValues(schema, map[string]any{"limit": value})
		if err != nil {
			t.Fatalf("normalize integer %#v: %v", value, err)
		}
		if normalized["limit"] != int64(42) {
			t.Fatalf("normalized integer %#v = %#v", value, normalized["limit"])
		}
	}
}

func TestNormalizeSchemaValuesAcceptsIntegerStringBoundaries(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "offset", Type: FieldInteger, Required: true}}}
	for _, value := range []json.Number{
		json.Number("-9223372036854775808"),
		json.Number("9223372036854775807"),
	} {
		normalized, err := NormalizeSchemaValues(schema, map[string]any{"offset": value})
		if err != nil {
			t.Fatalf("normalize boundary %s: %v", value, err)
		}
		if normalized["offset"] == nil {
			t.Fatalf("normalized boundary %s is missing", value)
		}
	}
}

func TestNormalizeSchemaValuesRejectsInexactOrOutOfRangeIntegers(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "limit", Type: FieldInteger, Required: true}}}
	values := []any{
		1.5,
		float32(1.5),
		"1.5",
		json.Number("1.5"),
		json.Number("9223372036854775808"),
		uint64(math.MaxInt64) + 1,
		float64(1 << 53),
		float64(1 << 63),
		math.NaN(),
		math.Inf(1),
	}
	for _, value := range values {
		if _, err := NormalizeSchemaValues(schema, map[string]any{"limit": value}); err == nil {
			t.Fatalf("expected integer %#v to be rejected", value)
		}
	}
}

func TestNativeIntValueRejectsInexactAndOutOfRangeValues(t *testing.T) {
	for _, value := range []any{1.5, json.Number("1.5"), "1.5", uint64(math.MaxInt64) + 1} {
		if parsed, ok := NativeIntValue(value); ok {
			t.Fatalf("NativeIntValue(%#v) = %d, true", value, parsed)
		}
	}

	for _, value := range []any{42, int64(42), float64(42), json.Number("42"), "42"} {
		if parsed, ok := NativeIntValue(value); !ok || parsed != 42 {
			t.Fatalf("NativeIntValue(%#v) = %d, %t", value, parsed, ok)
		}
	}
}

func TestValidateSchemaDefinitionRejectsInvalidIntegerDefault(t *testing.T) {
	err := ValidateNonSecretSchema(Schema{Fields: []Field{
		{Name: "limit", Type: FieldInteger, Default: 1.5},
	}}, "action")
	if err == nil || !strings.Contains(err.Error(), "invalid default") {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalizeSchemaValuesKeepsDecimalNumberSemantics(t *testing.T) {
	normalized, err := NormalizeSchemaValues(
		Schema{Fields: []Field{{Name: "ratio", Type: FieldNumber, Required: true}}},
		map[string]any{"ratio": "1.5"},
	)
	if err != nil || normalized["ratio"] != 1.5 {
		t.Fatalf("normalized decimal = %#v, err = %v", normalized, err)
	}
}

func TestNormalizeSchemaValuesAppliesDefaults(t *testing.T) {
	normalized, err := NormalizeSchemaValues(Schema{Fields: []Field{
		{Name: "host", Type: FieldString, Required: true},
		{Name: "port", Type: FieldNumber, Required: true, Default: 5432},
		{Name: "ssl_mode", Type: FieldSelect, Default: "require", Options: []FieldOption{{Value: "require", Label: "Require"}, {Value: "prefer", Label: "Prefer"}}},
	}}, map[string]any{
		"host": "localhost",
	})
	if err != nil {
		t.Fatalf("normalize schema values: %v", err)
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("marshal normalized values: %v", err)
	}
	if string(payload) != `{"host":"localhost","port":5432,"ssl_mode":"require"}` {
		t.Fatalf("normalized payload = %s", payload)
	}
}

func TestNormalizeSchemaValuesRejectsNonFiniteNumbers(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "port", Type: FieldNumber, Required: true}}}
	for _, value := range []any{"NaN", "Inf", "-Inf", math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := NormalizeSchemaValues(schema, map[string]any{"port": value}); err == nil {
			t.Fatalf("expected non-finite number %#v to be rejected", value)
		}
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
		{Name: "run", Label: "Run", Description: "Run once.", Risk: RiskRead},
		{Name: "run", Label: "Run again", Description: "Run again.", Risk: RiskRead},
	}, "test"); err == nil || !strings.Contains(err.Error(), "duplicate action") {
		t.Fatalf("expected duplicate action error, got %v", err)
	}
	if err := ValidateActionDefinitions([]ActionDefinition{{
		Name:        "run",
		Label:       "Run",
		Description: "Run once.",
		Risk:        RiskRead,
		InputSchema: Schema{Fields: []Field{
			{Name: "password", Type: FieldSecret, Secret: true},
		}},
	}}, "test"); err == nil || !strings.Contains(err.Error(), "must not be secret") {
		t.Fatalf("expected secret action input error, got %v", err)
	}
	if err := ValidateActionDefinitions([]ActionDefinition{{
		Name:        "run",
		Label:       "Run",
		Description: "Run once.",
		Risk:        RiskLevel("mystery"),
	}}, "test"); err == nil || !strings.Contains(err.Error(), "unsupported risk") {
		t.Fatalf("expected unsupported risk error, got %v", err)
	}
	if err := ValidateActionDefinitions([]ActionDefinition{{
		Name:        "run",
		Description: "Run once.",
		Risk:        RiskRead,
	}}, "test"); err == nil || !strings.Contains(err.Error(), "label is required") {
		t.Fatalf("expected missing label error, got %v", err)
	}
	if err := ValidateActionDefinitions([]ActionDefinition{{
		Name:  "run",
		Label: "Run",
		Risk:  RiskRead,
	}}, "test"); err == nil || !strings.Contains(err.Error(), "description is required") {
		t.Fatalf("expected missing description error, got %v", err)
	}
	if err := ValidateActionDefinitions([]ActionDefinition{{
		Name:                 "run",
		Label:                "Run",
		Description:          "Run once.",
		Risk:                 RiskWrite,
		InputSchema:          Schema{Fields: []Field{{Name: "message", Label: "Message", Type: FieldString}}},
		SensitiveInputFields: []string{"missing"},
	}}, "test"); err == nil || !strings.Contains(err.Error(), "not in its input schema") {
		t.Fatalf("expected unknown sensitive input field error, got %v", err)
	}
	if err := ValidateActionDefinitions([]ActionDefinition{{
		Name:        "run",
		Label:       "Run",
		Description: "Run once.",
		Risk:        RiskRead,
		OutputHint:  OutputHint{TemporaryCapabilityFields: []string{"signed.url"}},
	}}, "test"); err == nil || !strings.Contains(err.Error(), "invalid temporary capability field") {
		t.Fatalf("expected invalid temporary capability field error, got %v", err)
	}
	if err := ValidateActionDefinitions([]ActionDefinition{{
		Name:        "run",
		Label:       "Run",
		Description: "Run once.",
		Risk:        RiskRead,
		OutputHint: OutputHint{
			SensitiveFields:           []string{"url"},
			TemporaryCapabilityFields: []string{"url"},
		},
	}}, "test"); err == nil || !strings.Contains(err.Error(), "also marked sensitive") {
		t.Fatalf("expected ambiguous temporary capability field error, got %v", err)
	}
}
