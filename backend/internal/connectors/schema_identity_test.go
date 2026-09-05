package connectors

import "testing"

func TestOpaqueStringSchemaPreservesWhitespaceOnlyIdentity(t *testing.T) {
	for _, required := range []bool{false, true} {
		field := Field{Name: "key", Type: FieldString, Required: required, PreserveWhitespace: true}
		values, err := NormalizeSchemaValues(Schema{Fields: []Field{field}}, map[string]any{"key": " "})
		if err != nil || values["key"] != " " {
			t.Fatalf("opaque normalization: %v, %v", values, err)
		}
		field.PreserveWhitespace = false
		values, err = NormalizeSchemaValues(Schema{Fields: []Field{field}}, map[string]any{"key": " "})
		if required && err == nil {
			t.Fatal("ordinary required whitespace accepted")
		}
		if !required && (err != nil || len(values) != 0) {
			t.Fatal("ordinary optional whitespace changed")
		}
	}
}

func TestOpaqueCredentialStringAndCatalogEquality(t *testing.T) {
	field := Field{Name: "identity", Type: FieldString, Required: true, PreserveWhitespace: true}
	if err := ValidateCredentialSchemaValues(Schema{Fields: []Field{field}}, map[string]any{"identity": " "}, nil, false); err != nil {
		t.Fatal(err)
	}
	base := []ActionDefinition{{Name: "read", InputSchema: Schema{Fields: []Field{field}}}}
	field.PreserveWhitespace = false
	changed := []ActionDefinition{{Name: "read", InputSchema: Schema{Fields: []Field{field}}}}
	if ActionDefinitionsEqual(base, changed) {
		t.Fatal("identity schema drift ignored")
	}
	if err := ValidateCredentialSchemaValues(Schema{Fields: []Field{field}}, map[string]any{"identity": " "}, nil, false); err == nil {
		t.Fatal("ordinary credential validation changed")
	}
	field.Type, field.PreserveWhitespace = FieldInteger, true
	if err := ValidateNonSecretSchema(Schema{Fields: []Field{field}}, "test"); err == nil {
		t.Fatal("non-string flag accepted")
	}
}
