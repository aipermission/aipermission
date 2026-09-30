package observability

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

func TestAuditPayloadPreservesJSONAfterPreparedBasicRedaction(t *testing.T) {
	prepared := securitypolicy.NewService(nil).PrepareRedactor(t.Context())
	event, err := BuildEvent(t.Context(), nil, BuildInput{
		Action: "connector_action.completed",
		Payload: map[string]any{
			"password":   "unregistered-json-secret",
			"stdout":     `{"api_key":"unregistered-json-secret","keep":"visible"}`,
			"stderr":     `password="unregistered-json-secret with spaces"`,
			"request_id": 71,
		},
		Redact: prepared,
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil || payload["request_id"] != float64(71) || !strings.Contains(event.PayloadJSON, "visible") || strings.Contains(event.PayloadJSON, "unregistered-json-secret") {
		t.Fatalf("audit JSON projection lost metadata or leaked: %s, %v", event.PayloadJSON, err)
	}
}
