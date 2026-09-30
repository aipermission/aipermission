package actionresult

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

func TestConnectorResultMasksUnregisteredJSONTextSecrets(t *testing.T) {
	redact := securitypolicy.NewService(nil).Redact
	projector, err := NewRedactor(redact, redact, 1024)
	if err != nil {
		t.Fatal(err)
	}
	input := `{"password":"unregistered-json-secret","keep":"visible"}`
	result, err := projector.ResultWithCredentialBoundary(t.Context(), connectors.ActionResult{
		DisplayText: input, Error: input, Output: map[string]any{"stdout": input, "body": input, "content": []string{input}},
	}, NewCredentialBoundary(map[string]any{"password": "known-profile-credential"}))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "unregistered-json-secret") || !strings.Contains(string(encoded), "visible") {
		t.Fatalf("projected JSON text = %s, %v", encoded, err)
	}
	for _, value := range []string{result.DisplayText, result.Error} {
		if !json.Valid([]byte(value)) {
			t.Fatalf("JSON text no longer valid: %s", value)
		}
	}
}
