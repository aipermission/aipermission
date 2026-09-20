package connectors

import "testing"

func TestOpaqueStringSchemaPreservesWhitespaceOnlyIdentity(t *testing.T) {
	for _, fieldType := range []FieldType{FieldString, FieldMultiline} {
		for _, required := range []bool{false, true} {
			field := Field{Name: "key", Type: fieldType, Required: required, PreserveWhitespace: true}
			values, err := NormalizeSchemaValues(Schema{Fields: []Field{field}}, map[string]any{"key": " "})
			if err != nil || values["key"] != " " {
				t.Fatalf("opaque %s normalization: %v, %v", fieldType, values, err)
			}
			field.PreserveWhitespace = false
			values, err = NormalizeSchemaValues(Schema{Fields: []Field{field}}, map[string]any{"key": " "})
			if required && err == nil {
				t.Fatalf("ordinary required %s whitespace accepted", fieldType)
			}
			if !required && (err != nil || len(values) != 0) {
				t.Fatalf("ordinary optional %s whitespace changed", fieldType)
			}
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
