package api

import (
	"encoding/json"
	"testing"
)

func TestIntConfigValueBoundsParsedInputToNativeInt(t *testing.T) {
	config := map[string]any{
		"from_string": "22",
		"from_json":   json.Number("5432"),
		"overflow":    "9223372036854775808",
		"zero":        0,
	}

	if got := intConfigValue(config, "from_string", 99); got != 22 {
		t.Fatalf("from string = %d, want 22", got)
	}
	if got := intConfigValue(config, "from_json", 99); got != 5432 {
		t.Fatalf("from json = %d, want 5432", got)
	}
	if got := intConfigValue(config, "overflow", 99); got != 99 {
		t.Fatalf("overflow = %d, want fallback 99", got)
	}
	if got := intConfigValue(config, "zero", 99); got != 99 {
		t.Fatalf("zero = %d, want fallback 99", got)
	}
}
