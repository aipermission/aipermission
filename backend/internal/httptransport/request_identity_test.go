package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsLossyObjectResourceIdentitiesBeforeMutation(t *testing.T) {
	for _, body := range []string{
		`{"input":{"scope":{"schemas":[{"schema":"\ud800","tables":[]}]}}}`,
		`{"input":{"scope":{"schemas":[{"schema":"public","tables":[{"table":"users","columns":["\udfff"]}]}]}}}`,
		`{"input":{"\ud800":"value"}}`,
		`{"input":{"name":"` + string([]byte{0xff}) + `"}}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		var target struct {
			Input map[string]any `json:"input"`
		}
		if DecodeJSON(response, request, &target, 0) || response.Code != http.StatusBadRequest || target.Input != nil {
			t.Fatalf("invalid raw identity decoded or dispatched: target=%#v response=%s", target, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "users") || strings.Contains(response.Body.String(), "value") {
			t.Fatal("decode error exposed request content")
		}
	}
}

func TestDecodeJSONPreservesValidUnicodeAndNumericPrecision(t *testing.T) {
	for _, name := range []string{`\ufffd`, `\ud83d\ude80`, `\\ud800`} {
		body := `{"input":{"scope":{"schema":"` + name + `"},"number":9007199254740993}}`
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		var target struct {
			Input map[string]any `json:"input"`
		}
		if !DecodeJSON(httptest.NewRecorder(), request, &target, int64(len(body))) {
			t.Fatalf("valid identity rejected at exact body limit: %q", body)
		}
		var expected string
		if err := json.Unmarshal([]byte(`"`+name+`"`), &expected); err != nil {
			t.Fatal(err)
		}
		if target.Input["scope"].(map[string]any)["schema"] != expected || target.Input["number"] != json.Number("9007199254740993") {
			t.Fatalf("valid identity or numeric precision changed: %#v", target)
		}
	}
}
