package jsonnumber

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPublicValueNumericContract(t *testing.T) {
	for _, text := range []string{"0", "0e999999999", "1", "-1", "9007199254740991", "-9007199254740991", "0.1", "1.25", "1e-6", "1.2500"} {
		if got := PublicValue(json.Number(text)); got != json.Number(text) {
			t.Fatalf("safe number %q changed to %#v", text, got)
		}
	}
	for _, text := range []string{"-0", "-0.00", "9007199254740992", "9007199254740993", "-9007199254740993", "9223372036854775807", "-9223372036854775808", "18446744073709551615", "0.10000000000000000001", "1.234567890123456789", "1e100", "1e999999999", "1e-999999999", "1e-324", "4.9406564584124654e-324", strings.Repeat("1", 1025)} {
		if got := PublicValue(json.Number(text)); got != text {
			t.Fatalf("inexact number %q changed to %#v", text, got)
		}
	}
}

func TestPublicValueTraversesOnlyNumbersAndIsIdempotent(t *testing.T) {
	input := map[string]any{"rows": []any{map[string]any{"big": json.Number("9223372036854775807"), "safe": json.Number("3"), "text": "9007199254740993", "null": nil, "bool": true}}}
	got := PublicValue(input)
	want := map[string]any{"rows": []any{map[string]any{"big": "9223372036854775807", "safe": json.Number("3"), "text": "9007199254740993", "null": nil, "bool": true}}}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(PublicValue(got), want) {
		t.Fatalf("numeric projection changed nonnumeric data: %#v", got)
	}
}
