package actionresult

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCredentialBoundaryRedactsEncodedVariants(t *testing.T) {
	boundary := NewCredentialBoundary(map[string]any{
		"password": "credential+/value",
		"nested":   map[string]any{"token": "second-value"},
		"quoted":   `credential"value`,
	})
	for _, value := range []string{
		"credential+/value",
		"credential%2B%2Fvalue",
		"Y3JlZGVudGlhbCsvdmFsdWU=",
		"second-value",
		`credential\"value`,
	} {
		if redacted := boundary.Redact("prefix " + value + " suffix"); strings.Contains(redacted, value) || !strings.Contains(redacted, CredentialRedactionMarker) {
			t.Fatalf("credential variant was not redacted: input=%q output=%q", value, redacted)
		}
	}
	htmlBoundary := NewCredentialBoundary(map[string]any{"password": `<admin&"secret">`})
	if redacted := htmlBoundary.Redact(`remote said &lt;admin&amp;&#34;secret&#34;&gt;`); strings.Contains(redacted, "secret") {
		t.Fatalf("HTML-encoded credential was not redacted: %q", redacted)
	}
}

func TestCredentialBoundaryDoesNotCorruptTextForShortSecrets(t *testing.T) {
	boundary := NewCredentialBoundary(map[string]any{"password": "a"})
	if got := boundary.Redact("database action completed"); got != "database action completed" {
		t.Fatalf("short credential corrupted unrelated output: %q", got)
	}
	if got := boundary.Redact("a"); got != CredentialRedactionMarker {
		t.Fatalf("exact short credential scalar = %q, want redacted", got)
	}
	if got := boundary.Redact("authentication rejected a"); strings.Contains(got, "rejected a") {
		t.Fatalf("delimited one-byte credential remained visible: %q", got)
	}
	boundary = NewCredentialBoundary(map[string]any{"password": "abc"})
	if got := boundary.Redact("authentication rejected abc"); strings.Contains(got, "abc") {
		t.Fatalf("delimited three-byte credential remained visible: %q", got)
	}
	if got := boundary.Redact("prefixabcsuffix"); strings.Contains(got, "abc") {
		t.Fatalf("embedded three-byte credential remained visible: %q", got)
	}
	boundary = NewCredentialBoundary(map[string]any{"password": "secret"})
	if got := boundary.Redact("credential secret rejected"); strings.Contains(got, "secret") {
		t.Fatalf("delimited short credential remained visible: %q", got)
	}
	if got := boundary.RedactKey("customer_secret"); got != "customer_secret" {
		t.Fatalf("short credential corrupted an unrelated field name: %q", got)
	}
}

func TestCredentialBoundaryRedactsLabeledShortSecret(t *testing.T) {
	boundary := NewCredentialBoundary(map[string]any{"password": "x"})
	redacted := boundary.Redact(`remote rejected password=x and "password":"x"`)
	if strings.Contains(redacted, "password=x") || strings.Contains(redacted, `"password":"x"`) {
		t.Fatalf("short labeled credential was not redacted: %s", redacted)
	}
	if !strings.Contains(redacted, CredentialRedactionMarker) {
		t.Fatalf("short credential marker missing: %s", redacted)
	}
}

func TestCredentialBoundaryRedactsStructuredKeysAndValues(t *testing.T) {
	boundary := NewCredentialBoundary(map[string]any{"token": "credential-value-123"})
	redacted := boundary.RedactStructured(map[string]any{
		"credential-value-123": "credential-value-123",
		"nested":               []any{"safe", "credential-value-123"},
	}).(map[string]any)
	if _, exposed := redacted["credential-value-123"]; exposed {
		t.Fatal("credential remained visible as a structured key")
	}
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("marshal redacted output: %v", err)
	}
	if strings.Contains(string(encoded), "credential-value-123") {
		t.Fatalf("credential remained visible in structured output: %s", string(encoded))
	}
}
