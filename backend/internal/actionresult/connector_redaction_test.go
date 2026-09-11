package actionresult

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestRedactorFailsClosedWhenUnconfigured(t *testing.T) {
	if _, err := NewRedactor(nil, func(context.Context, string) string { return "" }, 1024); err == nil {
		t.Fatal("missing persistence redactor was accepted")
	}
	if _, err := NewRedactor(func(context.Context, string) string { return "" }, nil, 1024); err == nil {
		t.Fatal("missing capability redactor was accepted")
	}
	if _, err := NewRedactor(func(context.Context, string) string { return "" }, func(context.Context, string) string { return "" }, 0); err == nil {
		t.Fatal("invalid input limit was accepted")
	}
}

func TestRedactorAppliesWorkspaceCredentialAndSchemaBoundaries(t *testing.T) {
	redactor, err := NewRedactor(
		func(_ context.Context, value string) string {
			return strings.ReplaceAll(value, "workspace-secret", "[WORKSPACE]")
		},
		func(_ context.Context, value string) string {
			return strings.ReplaceAll(value, "temporary-capability", "[CAPABILITY]")
		},
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	boundary := NewCredentialBoundary(map[string]any{"secret": "credential-secret"})
	result, err := redactor.ResultWithCredentialBoundary(t.Context(), connectors.ActionResult{
		DisplayText: "workspace-secret credential-secret",
		Output: map[string]any{
			"password":   "schema-secret",
			"message":    "workspace-secret credential-secret",
			"capability": "temporary-capability",
		},
	}, boundary, connectors.OutputHint{TemporaryCapabilityFields: []string{"capability"}})
	if err != nil {
		t.Fatal(err)
	}
	encoded := strings.Join([]string{result.DisplayText, fmt.Sprint(result.Output)}, " ")
	for _, secret := range []string{"workspace-secret", "credential-secret", "schema-secret", "temporary-capability"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("redacted result contains %q: %s", secret, encoded)
		}
	}
}

func TestRedactorInputHonorsConfiguredEncodedLimit(t *testing.T) {
	redactor, err := NewRedactor(
		func(_ context.Context, value string) string { return value },
		func(_ context.Context, value string) string { return value },
		32,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := redactor.Input(t.Context(), map[string]any{"value": strings.Repeat("x", 64)}, nil); err == nil {
		t.Fatal("oversized connector input was accepted")
	}
}

func TestRedactorTreatsNilBoundaryAsEmpty(t *testing.T) {
	redactor, err := NewRedactor(
		func(_ context.Context, value string) string { return value },
		func(_ context.Context, value string) string { return value },
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := redactor.ResultWithCredentialBoundary(t.Context(), connectors.ActionResult{
		Status: connectors.ResultCompleted, DisplayText: "visible", Output: map[string]any{"value": "visible"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.DisplayText != "visible" || result.Output.(map[string]any)["value"] != "visible" {
		t.Fatalf("nil boundary changed output: %#v", result)
	}
}
