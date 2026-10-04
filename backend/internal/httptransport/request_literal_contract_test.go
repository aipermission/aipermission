package httptransport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCoreJSONDecoderPreservesValidUnicodeNames(t *testing.T) {
	for _, test := range []struct{ raw, want string }{
		{`invoice\ufffd`, "invoice\ufffd"},
		{`invoice\ud83d\ude00`, "invoice\U0001f600"},
		{`literal\\ud800`, `literal\ud800`},
	} {
		t.Run(test.raw, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewBufferString(`{"name":"`+test.raw+`"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			var decoded struct {
				Name string `json:"name"`
			}
			if !DecodeJSON(response, request, &decoded, DefaultJSONBodyBytes) || decoded.Name != test.want {
				t.Fatalf("exact Unicode identity changed: got=%q want=%q body=%s", decoded.Name, test.want, response.Body.String())
			}
		})
	}
}

func TestCoreJSONDecoderRejectsUnknownAndTrailingFields(t *testing.T) {
	for _, raw := range []string{`{"name":"ok","extra":true}`, `{"name":"ok"} {}`} {
		request := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		var decoded struct {
			Name string `json:"name"`
		}
		if DecodeJSON(response, request, &decoded, DefaultJSONBodyBytes) || response.Code != http.StatusBadRequest {
			t.Fatalf("non-strict JSON accepted: %s", raw)
		}
		var body ErrorResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error != "invalid json body" {
			t.Fatalf("unstable or revealing error: body=%s err=%v", response.Body.String(), err)
		}
	}
}
