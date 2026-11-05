package actionresult

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestFinalOutputLimitAppliesAfterPublicProjection(t *testing.T) {
	redactor, err := NewRedactor(func(_ context.Context, text string) string {
		return strings.ReplaceAll(text, "hide", "[REDACTED]")
	}, func(_ context.Context, text string) string { return text }, 1024)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		output    any
		limit     int
		wantError bool
	}{
		{"exact bytes", "hello", 7, false},
		{"over bytes", "hello", 6, true},
		{"redaction expands", "hide", 6, true},
		{"redaction fits", "hide", 12, false},
		{"numeric projection expands", json.Number("9007199254740993"), 16, true},
		{"numeric projection fits", json.Number("9007199254740993"), 18, false},
		{"unicode encoded bytes", "é", 4, false},
		{"empty output", nil, 4, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := redactor.Result(t.Context(), connectors.ActionResult{Status: connectors.ResultCompleted, Output: tc.output}, connectors.OutputHint{FinalMaxBytes: tc.limit})
			if errors.Is(err, ErrInvalidOutput) != tc.wantError {
				t.Fatalf("projection error=%v wantError=%v", err, tc.wantError)
			}
			if tc.wantError {
				if result.Output != nil || result.Status != "" {
					t.Fatalf("oversized projection escaped: %#v", result)
				}
				return
			}
			encoded, err := json.Marshal(result.Output)
			if err != nil || len(encoded) > tc.limit {
				t.Fatalf("invalid final bytes: %q %v", encoded, err)
			}
		})
	}
	input := connectors.ActionResult{Output: "hello"}
	if _, err := redactor.Result(t.Context(), input, connectors.OutputHint{MaxBytes: 4}); err != nil {
		t.Fatalf("legacy rendering hint became a hard output limit: %v", err)
	}
	if _, err := redactor.Result(t.Context(), input, connectors.OutputHint{FinalMaxBytes: 8}, connectors.OutputHint{FinalMaxBytes: 6}); !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("less restrictive hint overrode the smaller final cap: %v", err)
	}
}
