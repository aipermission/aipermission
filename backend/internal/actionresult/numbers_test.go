package actionresult

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestCanonicalResultPreservesJavaScriptNumericData(t *testing.T) {
	input := map[string]any{"big": int64(math.MaxInt64), "small": 42, "decimal": json.Number("0.1234567890123456789")}
	got, err := Canonicalize(input, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil || string(encoded) != `{"big":"9223372036854775807","decimal":"0.1234567890123456789","small":42}` {
		t.Fatalf("first response changed numeric data: %s %v", encoded, err)
	}
	if input["big"] != int64(math.MaxInt64) {
		t.Fatal("public projection mutated the source connector value")
	}
}

func TestNumericProjectionStillEnforcesSizeLimits(t *testing.T) {
	for _, limits := range []Limits{{StringBytes: 5}, {EncodedBytes: 16}} {
		_, err := Canonicalize(json.Number("9007199254740993"), limits)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("numeric string expansion bypassed limits: %+v %v", limits, err)
		}
	}
}

func TestProjectedNumericStringsStillPassThroughRedaction(t *testing.T) {
	output, err := CanonicalizeAndRedact(json.Number("9007199254740993"), DefaultLimits(), RedactionOptions{
		RedactText: func(text string) string { return strings.ReplaceAll(text, "9007199254740993", "[REDACTED]") },
	})
	if err != nil || output != "[REDACTED]" {
		t.Fatalf("numeric projection bypassed string redaction: %#v %v", output, err)
	}
}
