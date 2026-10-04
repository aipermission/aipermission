package actionresult

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlresult"
)

func TestResultRedactsNumericCredentialsWithoutOptionalRedaction(t *testing.T) {
	identity := func(_ context.Context, value string) string { return value }
	redactor, err := NewRedactor(identity, identity, 1024)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"7", "1234567890123", "9007199254740993", "-12345", "1.25", "1e3"} {
		t.Run(secret, func(t *testing.T) {
			boundary := NewCredentialBoundary(map[string]any{"password": secret})
			result, err := redactor.ResultWithCredentialBoundary(t.Context(), connectors.ActionResult{
				Output: map[string]any{
					"rows":       []any{[]any{json.Number(secret)}},
					"safe":       json.Number("42.5"),
					"capability": []any{json.Number(secret)},
				},
				Metadata: map[string]any{"observed": json.Number(secret)},
			}, boundary, connectors.OutputHint{TemporaryCapabilityFields: []string{"capability"}})
			if err != nil {
				t.Fatal(err)
			}
			output := result.Output.(map[string]any)
			if got := output["rows"].([]any)[0].([]any)[0]; got != CredentialRedactionMarker {
				t.Fatalf("numeric credential survived: %#v", got)
			}
			if output["safe"] != json.Number("42.5") || result.Metadata["observed"] != CredentialRedactionMarker || output["capability"].([]any)[0] != CredentialRedactionMarker {
				t.Fatalf("numeric projection or metadata boundary failed: %#v", result)
			}
			repeated, err := redactor.ResultWithCredentialBoundary(t.Context(), result, boundary)
			if err != nil || !reflect.DeepEqual(result, repeated) {
				t.Fatalf("repeated projection changed result: %#v, %v", repeated, err)
			}
			encoded, err := json.Marshal(result)
			if err != nil || !strings.Contains(string(encoded), `"safe":42.5`) {
				t.Fatalf("safe number changed on delivery: %s, %v", encoded, err)
			}
		})
	}
}

func TestOptionalTextRulesDoNotRewriteUnmatchedNumbers(t *testing.T) {
	rewrite := func(_ context.Context, _ string) string { return "[OPTIONAL]" }
	redactor, err := NewRedactor(rewrite, rewrite, 1024)
	if err != nil {
		t.Fatal(err)
	}
	value, err := redactor.ValueWithCredentialBoundary(t.Context(), json.Number("42.5"), nil, nil, NewCredentialBoundary(nil))
	if err != nil || value != json.Number("42.5") {
		t.Fatalf("optional text policy rewrote numeric data: %#v, %v", value, err)
	}
}

func TestCredentialStructuredBoundaryMasksNumericLeaves(t *testing.T) {
	boundary := NewCredentialBoundary(map[string]any{"password": "1234567890123"})
	for _, numeric := range []any{json.Number("1234567890123"), int64(1234567890123), uint64(1234567890123), float64(1234567890123)} {
		value, err := boundary.RedactStructured(map[string]any{"observed": []any{numeric, int64(42)}})
		if err != nil {
			t.Fatal(err)
		}
		leaves := value.(map[string]any)["observed"].([]any)
		if leaves[0] != CredentialRedactionMarker || leaves[1] != int64(42) {
			t.Fatalf("structured numeric boundary failed for %T: %#v", numeric, leaves)
		}
	}
}

func TestSQLBuilderResultMasksNumericCredentialCell(t *testing.T) {
	builder := sqlresult.NewBuilder([]string{"observed", "safe"}, 10, 4096, 1024, "...")
	if !builder.Add([]any{int64(1234567890123), int64(42)}, func(value any) any { return value }) {
		t.Fatal("SQL builder rejected the fixture row")
	}
	identity := func(_ context.Context, value string) string { return value }
	redactor, err := NewRedactor(identity, identity, 1024)
	if err != nil {
		t.Fatal(err)
	}
	result, err := redactor.ResultWithCredentialBoundary(t.Context(), connectors.ActionResult{
		Output: builder.Result(nil).ToMap(4096, nil),
	}, NewCredentialBoundary(map[string]any{"password": "1234567890123"}))
	if err != nil {
		t.Fatal(err)
	}
	row := result.Output.(map[string]any)["rows"].([]any)[0].(map[string]any)
	if row["observed"] != CredentialRedactionMarker || row["safe"] != json.Number("42") {
		t.Fatalf("SQL cell boundary failed: %#v", row)
	}
}

func TestScalarCredentialRegistrationBeforeSecretAccess(t *testing.T) {
	for _, secret := range []any{json.Number("9007199254740993"), true, false, int(7), int8(7), int16(7), int32(7), int64(7), uint(7), uint8(7), uint16(7), uint32(7), uint64(7), float32(7.5), float64(1e9)} {
		t.Run(fmt.Sprintf("%T-%v", secret, secret), func(t *testing.T) {
			boundary := NewCredentialBoundary(map[string]any{"nested": []any{secret}})
			if boundary.Empty() || boundary.Redact(fmt.Sprint(secret)) != CredentialRedactionMarker {
				t.Fatal("stored scalar credential was not registered before connector access")
			}
			projected, err := boundary.RedactStructured(secret)
			if err != nil || projected != CredentialRedactionMarker {
				t.Fatalf("approval scalar leaked: %#v, %v", projected, err)
			}
			identity := func(_ context.Context, value string) string { return value }
			redactor, err := NewRedactor(identity, identity, 1024)
			if err != nil {
				t.Fatal(err)
			}
			projected, err = redactor.ValueWithCredentialBoundary(t.Context(), secret, nil, nil, boundary)
			if err != nil || projected != CredentialRedactionMarker {
				t.Fatalf("result scalar leaked: %#v, %v", projected, err)
			}
		})
	}
}

func TestPrecisionProjectedStringsRetainOptionalTextPolicy(t *testing.T) {
	rewrite := func(_ context.Context, value string) string {
		return strings.Map(func(char rune) rune {
			if char >= '0' && char <= '9' {
				return 'x'
			}
			return char
		}, value)
	}
	redactor, err := NewRedactor(rewrite, rewrite, 1024)
	if err != nil {
		t.Fatal(err)
	}
	result, err := redactor.Result(t.Context(), connectors.ActionResult{
		Output:   map[string]any{"safe": json.Number("42.5"), "zero": json.Number("-0"), "decimal": json.Number("0.1234567890123456789"), "capability": json.Number("1e999"), "boolean": true},
		Metadata: map[string]any{"integer": json.Number("9007199254740993")},
	}, connectors.OutputHint{TemporaryCapabilityFields: []string{"capability"}})
	if err != nil {
		t.Fatal(err)
	}
	output := result.Output.(map[string]any)
	if output["safe"] != json.Number("42.5") || output["zero"] != "-x" || output["boolean"] != true || output["decimal"] != "x.xxxxxxxxxxxxxxxxxxx" || output["capability"] != "xexxx" || result.Metadata["integer"] != "xxxxxxxxxxxxxxxx" {
		t.Fatalf("numeric/string policy distinction changed: %#v", result)
	}
	repeated, err := redactor.Result(t.Context(), result, connectors.OutputHint{TemporaryCapabilityFields: []string{"capability"}})
	if err != nil || !reflect.DeepEqual(result, repeated) {
		t.Fatalf("repeated text-policy projection changed: %#v, %v", repeated, err)
	}
}
