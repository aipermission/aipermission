package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCoreJSONRoutesRejectLossyResourceNames(t *testing.T) {
	fixture := newAPITestFixture(t)
	token := createAPITestToken(t, fixture, t.Context(), "unicode boundary")
	var initialCount int
	if err := fixture.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM projects").Scan(&initialCount); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`invoice\ud800`, `invoice\udc00`, "invoice" + string([]byte{0xff})} {
		for _, path := range []string{"/api/projects", "/api/connector-actions/local-run", "/api/mcp/connector-actions/call"} {
			t.Run(path+"/"+name, func(t *testing.T) {
				body := `{"name":"` + name + `"}`
				if strings.Contains(path, "connector-actions") {
					body = `{"target_ref":"fixture:1:1","action_name":"delete_object","input":{"key":"` + name + `"},"reason":"identity test","idempotency_key":"identity-test"}`
				}
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				request.Host, request.RemoteAddr = "localhost:8080", "127.0.0.1:12345"
				request.Header.Set("Content-Type", "application/json")
				if strings.Contains(path, "/mcp/") {
					request.Header.Set("X-API-Key", token.TokenValue)
				} else {
					attachTestUIAuthorization(request)
				}
				response := httptest.NewRecorder()
				fixture.server.Handler().ServeHTTP(response, request)
				if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid json body") {
					t.Fatalf("malformed identity reached route handling: status=%d body=%s", response.Code, response.Body.String())
				}
			})
		}
	}
	var count int
	if err := fixture.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM projects").Scan(&count); err != nil || count != initialCount {
		t.Fatalf("invalid project name was persisted: count=%d err=%v", count, err)
	}
}

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
			if !decodeJSON(response, request, &decoded) || decoded.Name != test.want {
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
		if decodeJSON(response, request, &decoded) || response.Code != http.StatusBadRequest {
			t.Fatalf("non-strict JSON accepted: %s", raw)
		}
		var body errorResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error != "invalid json body" {
			t.Fatalf("unstable or revealing error: body=%s err=%v", response.Body.String(), err)
		}
	}
}
