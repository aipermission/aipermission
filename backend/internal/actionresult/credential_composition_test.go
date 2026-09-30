package actionresult

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

func TestCredentialProjectionPrecedesOptionalMasking(t *testing.T) {
	for _, secret := range []string{"alpha violet-suffix-7291", "blue\nlast-line-secret-914", "alpha [REDACTED CREDENTIAL]violet-suffix-7291"} {
		t.Run(secret, func(t *testing.T) {
			optional := func(_ context.Context, value string) string { return securitypolicy.RedactBasic(value) }
			redactor, err := NewRedactor(optional, optional, 4096)
			if err != nil {
				t.Fatal(err)
			}
			boundary := NewCredentialBoundary(map[string]any{"password": secret})
			value := "password=" + secret
			want := "password=" + CredentialRedactionMarker
			text, err := redactor.Text(t.Context(), value, boundary)
			if err != nil || text != want {
				t.Fatalf("text = %q, %v; want %q", text, err, want)
			}
			result, err := redactor.ResultWithCredentialBoundary(t.Context(), connectors.ActionResult{
				DisplayText: value, Error: value,
				Output:   map[string]any{"message": value, "capability": value, value: "visible"},
				Metadata: map[string]any{"message": value},
			}, boundary, connectors.OutputHint{TemporaryCapabilityFields: []string{"capability"}})
			if err != nil {
				t.Fatal(err)
			}
			if result.DisplayText != want || result.Error != want {
				t.Fatalf("display/error = %q / %q", result.DisplayText, result.Error)
			}
			output := result.Output.(map[string]any)
			if output["message"] != want || output["capability"] != want || output[want] != "visible" {
				t.Fatalf("structured text/key/capability = %#v", output)
			}
			if result.Metadata["message"] != want {
				t.Fatalf("metadata = %#v", result.Metadata)
			}
			repeated, err := redactor.ResultWithCredentialBoundary(t.Context(), result, boundary,
				connectors.OutputHint{TemporaryCapabilityFields: []string{"capability"}})
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(result)
			after, _ := json.Marshal(repeated)
			if string(before) != string(after) {
				t.Fatalf("projection is not idempotent: %s / %s", before, after)
			}
		})
	}
}

func TestCredentialProjectionPreservesMarkerAndStillAppliesCustomRules(t *testing.T) {
	optional := func(_ context.Context, value string) string {
		value = securitypolicy.RedactBasic(value)
		return strings.ReplaceAll(value, "custom-sensitive", "[CUSTOM]")
	}
	redactor, err := NewRedactor(optional, optional, 4096)
	if err != nil {
		t.Fatal(err)
	}
	boundary := NewCredentialBoundary(map[string]any{"password": "alpha violet-suffix-7291"})
	got, err := redactor.Text(t.Context(), "password=alpha violet-suffix-7291; custom-sensitive", boundary)
	want := "password=" + CredentialRedactionMarker + "; [CUSTOM]"
	if err != nil || got != want {
		t.Fatalf("text = %q, %v; want %q", got, err, want)
	}
}

func TestCredentialBoundaryDoesNotRewriteItsOwnMarker(t *testing.T) {
	boundary := NewCredentialBoundary(map[string]any{"password": "CREDENTIAL", "token": "REDACTED"})
	value := "password=CREDENTIAL token=REDACTED"
	want := "password=" + CredentialRedactionMarker + " token=" + CredentialRedactionMarker
	if got := boundary.Redact(value); got != want {
		t.Fatalf("text = %q; want %q", got, want)
	}
	if got := boundary.Redact(want); got != want {
		t.Fatalf("repeated text = %q; want %q", got, want)
	}
	if got := boundary.RedactKey(want); got != want {
		t.Fatalf("key = %q; want %q", got, want)
	}
}

func TestCredentialBoundaryMasksMatchesAcrossMarkerEdges(t *testing.T) {
	for _, test := range []struct{ secret, value string }{
		{"CREDENTIAL]synthetic-secret-7291", "note=" + CredentialRedactionMarker + "synthetic-secret-7291"},
		{"synthetic-secret-7291[REDACTED", "note=synthetic-secret-7291" + CredentialRedactionMarker},
		{"alpha " + CredentialRedactionMarker + " suffix", "note=alpha " + CredentialRedactionMarker + " suffix"},
	} {
		boundary := NewCredentialBoundary(map[string]any{"password": test.secret})
		for _, redact := range []func(string) string{boundary.Redact, boundary.RedactKey} {
			if got := redact(test.value); strings.Contains(got, test.secret) {
				t.Fatalf("credential spanning marker survived projection: %q", got)
			}
		}
	}
}

func TestCredentialCompositionRetainsWholeTextCustomPolicy(t *testing.T) {
	rule := regexp.MustCompile(`(?s)^password=.* customer=.*$`)
	optional := func(_ context.Context, value string) string {
		return rule.ReplaceAllString(securitypolicy.RedactBasic(value), "[REDACTED]")
	}
	redactor, err := NewRedactor(optional, optional, 4096)
	if err != nil {
		t.Fatal(err)
	}
	boundary := NewCredentialBoundary(map[string]any{"password": "alpha violet-suffix-7291"})
	got, err := redactor.Text(t.Context(), "password=alpha violet-suffix-7291 customer=private-customer-record", boundary)
	if err != nil || got != "[REDACTED]" {
		t.Fatalf("whole-text custom policy = %q, %v", got, err)
	}
}

func TestCredentialCompositionGuardsOptionalTransformationOutput(t *testing.T) {
	const secret = "alpha violet-suffix-7291"
	boundary := NewCredentialBoundary(map[string]any{"password": secret})
	got := RedactCredentialText("visible", boundary.Redact, func(string) string { return secret })
	if got != CredentialRedactionMarker {
		t.Fatalf("optional transformation exposed credential: %q", got)
	}
}
