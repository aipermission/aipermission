package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResponseWritersPreservePrimitiveWireContracts(t *testing.T) {
	for _, test := range []struct {
		name   string
		write  func(http.ResponseWriter)
		status int
		body   string
	}{
		{
			name: "json",
			write: func(w http.ResponseWriter) {
				WriteJSON(w, http.StatusAccepted, map[string]any{"id": json.Number("9007199254740993"), "name": "caf\u00e9"})
			},
			status: http.StatusAccepted,
			body:   "{\"id\":9007199254740993,\"name\":\"caf\u00e9\"}\n",
		},
		{
			name: "uncoded",
			write: func(w http.ResponseWriter) {
				WriteError(w, http.StatusBadRequest, "refused <value>", "")
			},
			status: http.StatusBadRequest,
			body:   `{"error":"refused \u003cvalue\u003e"}` + "\n",
		},
		{
			name: "coded",
			write: func(w http.ResponseWriter) {
				WriteError(w, http.StatusConflict, "changed", "approval_changed")
			},
			status: http.StatusConflict,
			body:   `{"error":"changed","code":"approval_changed"}` + "\n",
		},
		{
			name:   "internal",
			write:  WriteInternalError,
			status: http.StatusInternalServerError,
			body:   `{"error":"internal server error"}` + "\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			response.Header().Set("Cache-Control", "no-store, private")
			test.write(response)
			if response.Code != test.status || response.Body.String() != test.body || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("primitive wire contract changed: %d %q %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store, private" {
				t.Fatal("writer erased the caller's response policy")
			}
		})
	}
}
