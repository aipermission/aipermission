package gatewayvault

import (
	"reflect"
	"strings"
	"testing"
)

func TestPreparedRequestProjectionRedactorPreservesProtocolFields(t *testing.T) {
	secret := "description"
	prepared := PrepareRequestProjectionRedactor(func(value string) string { return strings.ReplaceAll(value, secret, "[REDACTED]") })
	if prepared == nil {
		t.Fatal("valid projection redactor was unavailable")
	}
	value := map[string]any{"description": "uses description", "name": "description"}
	output, err := prepared(t.Context(), value)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"description": "uses [REDACTED]", "name": "[REDACTED]"}
	if !reflect.DeepEqual(output, want) {
		t.Fatalf("prepared projection=%#v", output)
	}
	if value["description"] != "uses description" {
		t.Fatal("redaction changed sealed source values")
	}
	if PrepareRequestProjectionRedactor(nil) != nil {
		t.Fatal("missing policy must not create an unmasked projection")
	}
}
