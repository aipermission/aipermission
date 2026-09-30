package securitypolicy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactBasicJSONAfterIncompleteQuote(t *testing.T) {
	input := "\"\":\"\npassword=unregistered-json-secret\n{\"password\":\"unregistered-json-secret\"}"
	got := RedactBasic(input)
	if strings.Contains(got, "unregistered-json-secret") || !strings.HasSuffix(got, `{"password":"[REDACTED]"}`) {
		t.Fatalf("invalid earlier fragment swallowed JSON masking: %s", got)
	}
}

func TestRedactBasicQuotedJSONSecrets(t *testing.T) {
	for _, input := range []string{
		`{"password":"unrelated-json-secret"}`,
		`{"token" : "unrelated-json-secret", "count":9007199254740993}`,
		`{"api_key":"unrelated-json-secret with spaces and \"quotes\""}`,
		`{"pass\u0077ord":"unrelated-json-secret"}`,
		`{"nested":[{"secret":"unrelated-json-secret"}],"keep":"visible"}`,
		`{"stdout":"{\"password\":\"unrelated-json-secret\"}"}`,
		`{"body":"log: {\"password\":\"unrelated-json-secret\"}"}`,
		`{"body":"log: [\"password=\\\"unrelated-json-secret with spaces\\\"\"]"}`,
		`{"stdout":["password=\"unrelated-json-secret with spaces\""]}`,
		`[{"password":"unrelated-json-secret"}, "token=unrelated-json-secret"]`,
	} {
		t.Run(input, func(t *testing.T) {
			got := RedactBasic(input)
			if strings.Contains(got, "unrelated-json-secret") || !json.Valid([]byte(got)) {
				t.Fatalf("JSON secret survived or output is invalid: %s", got)
			}
			if second := RedactBasic(got); second != got {
				t.Fatalf("JSON redaction not idempotent: %s -> %s", got, second)
			}
		})
	}
}

func TestRedactBasicMixedJSONPreservesPlaintextRules(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{`password='{"keep":"alpha beta"}'`, `password='[REDACTED]'`},
		{`log: ["password=\"alpha beta\""]`, `log: ["password=\"[REDACTED]\""]`},
		{`log: {"password":12345}`, `log: {"password":"[REDACTED]"}`},
		{`log: "password":"passwd=` + "\x18" + `"`, `log: "password":"[REDACTED]"`},
	} {
		if got := RedactBasic(test.input); got != test.want {
			t.Fatalf("mixed JSON redaction = %q; want %q", got, test.want)
		}
	}
}

func TestRedactBasicJSONKeysKeepPatternMasking(t *testing.T) {
	for _, key := range []string{"ghp_abcdefghijklmnopqrstuvwxyz123456", "Bearer abc.def-123", "-----BEGIN PRIVATE KEY-----\nsynthetic\n-----END PRIVATE KEY-----"} {
		encoded, _ := json.Marshal(map[string]string{key: "visible"})
		for _, prefix := range []string{"", "log: "} {
			got := RedactBasic(prefix + string(encoded))
			if strings.Contains(got, "abcdefghijklmnopqrstuvwxyz123456") || strings.Contains(got, "abc.def-123") || strings.Contains(got, "synthetic") || !strings.Contains(got, "visible") {
				t.Fatalf("JSON key bypassed pattern rules: %s", got)
			}
		}
	}
}

func TestRedactBasicJSONBoundsNestedStringDecoding(t *testing.T) {
	input := `"unregistered-json-secret"`
	got := redactBasicJSONText(input, maxJSONRedactionDepth-1)
	if strings.Contains(got, "unregistered-json-secret") || !json.Valid([]byte(got)) || RedactBasic(got) != got {
		t.Fatalf("deep JSON text did not fail closed with stable valid JSON")
	}
	if redactBasicJSONText(input, maxJSONRedactionDepth) != redactionFailureMarker {
		t.Fatal("JSON string decoder did not enforce its nesting bound")
	}
}

func TestRedactBasicJSONPWDStillMasksProviderTokens(t *testing.T) {
	input := `{"PWD":"/home/ghp_abcdefghijklmnopqrstuvwxyz123456"}`
	if got := RedactBasic(input); got != `{"PWD":"/home/ghp_[REDACTED]"}` {
		t.Fatalf("PWD exception bypassed provider token masking: %s", got)
	}
}

func TestRedactBasicJSONParseBudgetFailsClosed(t *testing.T) {
	input := strings.Repeat("{", 32) + `{"password":12345}`
	if got := RedactBasic(input); got != redactionFailureMarker || RedactBasic(got) != got {
		t.Fatalf("exhausted JSON parse budget did not fail closed: %q", got)
	}
	input = `{"keep":"visible"} ` + input
	want := `{"keep":"visible"}` + redactionFailureMarker
	if got := RedactBasic(input); got != want || RedactBasic(got) != got {
		t.Fatalf("parse exhaustion changed an earlier complete JSON document: %q", got)
	}
}

func TestRedactBasicJSONMarkersDoNotExhaustParseBudget(t *testing.T) {
	input := strings.Repeat("[REDACTED PRIVATE KEY] ", 40) + `{"password":12345}`
	want := strings.Repeat("[REDACTED PRIVATE KEY] ", 40) + `{"password":"[REDACTED]"}`
	if got := RedactBasic(input); got != want || RedactBasic(got) != got {
		t.Fatalf("existing markers consumed the JSON discovery budget: %q", got)
	}
}

func TestRedactBasicJSONPreservesNonSecretsAndMarkers(t *testing.T) {
	for _, input := range []string{
		`{"PWD":"/home/developer","count":9007199254740993,"keep":true}`,
		`{"password":"[REDACTED CREDENTIAL]","api_key":"[REDACTED VAULT VALUE]"}`,
		`{"password_hint":"visible","public_key":"visible","empty":""}`,
		`{"keep":1,"keep":2,"count":9007199254740993}`,
	} {
		if got := RedactBasic(input); got != input {
			t.Fatalf("non-secret JSON changed: %s -> %s", input, got)
		}
	}
}

func TestRedactBasicJSONNonStringSecretsAndDuplicateKeys(t *testing.T) {
	input := `{"password":12345,"token":{"nested":"hidden"},"keep":1,"keep":2,"count":9007199254740993}`
	want := `{"password":"[REDACTED]","token":"[REDACTED]","keep":1,"keep":2,"count":9007199254740993}`
	if got := RedactBasic(input); got != want {
		t.Fatalf("JSON structure or field redaction changed: %s; want %s", got, want)
	}
}

func TestRedactBasicJSONInLogText(t *testing.T) {
	input := `time=now body={"password":"unrelated-json-secret", "keep":"visible"} end`
	got := RedactBasic(input)
	if strings.Contains(got, "unrelated-json-secret") || !strings.Contains(got, `"keep":"visible"`) || !strings.HasPrefix(got, "time=now body=") || !strings.HasSuffix(got, " end") {
		t.Fatalf("JSON log not safely redacted: %s", got)
	}
}

func TestPreparedRedactorMasksJSON(t *testing.T) {
	service := NewService(openTestDatabase(t))
	input := `{"password":"unrelated-json-secret","stdout":"{\"token\":\"unrelated-json-secret\"}"}`
	for _, redact := range []func(string) string{service.PrepareRedactor(t.Context()), func(value string) string { return service.Redact(t.Context(), value) }} {
		got := redact(input)
		if strings.Contains(got, "unrelated-json-secret") || !json.Valid([]byte(got)) {
			t.Fatalf("prepared/service JSON not masked: %s", got)
		}
	}
}
