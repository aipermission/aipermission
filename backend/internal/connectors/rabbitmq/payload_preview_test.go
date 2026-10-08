package rabbitmqconnector

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestPeekBase64PreviewUsesRawByteBudget(t *testing.T) {
	for _, size := range []int{16, 32, 40} {
		t.Run(string(rune('A'+size)), func(t *testing.T) {
			raw := bytes.Repeat([]byte{255}, size)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["truncate"] != float64(32) {
					t.Errorf("request body = %#v, error = %v", body, err)
				}
				_ = json.NewEncoder(w).Encode([]map[string]any{{"payload": base64.StdEncoding.EncodeToString(raw), "payload_encoding": "base64"}})
			}))
			defer server.Close()
			result, err := (Connector{}).ExecuteAction(t.Context(), testRuntimeForServer(t, server), connectors.PreparedAction{
				ActionName: ActionPeekMessages, Payload: map[string]any{"queue": "jobs", "max_payload_bytes": 32},
			})
			if err != nil {
				t.Fatal(err)
			}
			row := result.Output.(map[string]any)["messages"].([]map[string]any)[0]
			decoded, err := base64.StdEncoding.DecodeString(row["payload"].(string))
			if err != nil || !bytes.Equal(decoded, raw[:min(size, 32)]) || row["payload_truncated_by_gateway"] != (size > 32) {
				t.Fatalf("preview = %#v, decoded = %x, error = %v", row, decoded, err)
			}
		})
	}
}

func TestMessagePreviewRejectsInvalidBase64AndBoundsUTF8(t *testing.T) {
	if err := boundMessagePreview(map[string]any{"payload": "broken[truncated]", "payload_encoding": "base64"}, 32); err == nil {
		t.Fatal("accepted corrupt Base64")
	}
	row := map[string]any{"payload": "a\u00e9b", "payload_encoding": "string"}
	if err := boundMessagePreview(row, 2); err != nil {
		t.Fatal(err)
	}
	if got := row["payload"].(string); got != "a" || !utf8.ValidString(got) {
		t.Fatalf("UTF-8 preview = %q", got)
	}
}
