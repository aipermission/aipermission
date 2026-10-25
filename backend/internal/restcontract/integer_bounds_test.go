package restcontract

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

func TestMessageResponseEnforcesPublishedIntegerBounds(t *testing.T) {
	for _, value := range []string{"-1", "0", "100", "101", "1.5", "9223372036854775808", `"1"`, "null"} {
		t.Run(value, func(t *testing.T) {
			err := ValidateTypedResponse("POST", "/api/messages/read", 200, []byte(fmt.Sprintf(`{"status":"read","count":%s}`, value)))
			valid := value == "0" || value == "100"
			if (err == nil) != valid {
				t.Fatalf("count=%s validation=%v; valid=%t", value, err, valid)
			}
		})
	}
}

func TestIntegerBoundsPreserveExactInt64AndRejectInvalidSchemas(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  any
		schema map[string]any
		valid  bool
	}{
		{"exact minimum", json.Number("9223372036854775807"), map[string]any{"minimum": int64(math.MaxInt64)}, true},
		{"below exact minimum", json.Number("9223372036854775806"), map[string]any{"minimum": int64(math.MaxInt64)}, false},
		{"exact maximum", json.Number("-9223372036854775808"), map[string]any{"maximum": int64(math.MinInt64)}, true},
		{"above exact maximum", json.Number("-9223372036854775807"), map[string]any{"maximum": int64(math.MinInt64)}, false},
		{"fractional bound", json.Number("1"), map[string]any{"minimum": 0.5}, false},
		{"string bound", json.Number("1"), map[string]any{"maximum": "10"}, false},
		{"overflowing bound", json.Number("1"), map[string]any{"minimum": uint64(math.MaxUint64)}, false},
		{"invalid bound", json.Number("1"), map[string]any{"minimum": make(chan int)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.schema["type"] = "integer"
			err := validateSchemaValue("$", tc.value, tc.schema, nil)
			if (err == nil) != tc.valid {
				t.Fatalf("validation=%v; valid=%t", err, tc.valid)
			}
		})
	}
}
