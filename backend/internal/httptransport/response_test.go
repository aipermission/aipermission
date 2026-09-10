package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteErrorUsesStableJSONEnvelope(t *testing.T) {
	response := httptest.NewRecorder()
	WriteError(response, http.StatusBadRequest, "invalid request")
	if response.Code != http.StatusBadRequest || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response = %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	var body ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "invalid request" || body.Code != "" {
		t.Fatalf("body = %#v", body)
	}
}

func TestWriteSensitiveJSONPreventsCaching(t *testing.T) {
	response := httptest.NewRecorder()
	WriteSensitiveJSON(response, http.StatusOK, map[string]string{"value": "secret"})
	if response.Header().Get("Cache-Control") != "no-store, private" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
	if response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("Pragma = %q", response.Header().Get("Pragma"))
	}
}

func TestPositiveInt64ParsersRejectMalformedValues(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "not-a-number"} {
		response := httptest.NewRecorder()
		if _, ok := ParsePositiveInt64(response, value, "invalid id"); ok || response.Code != http.StatusBadRequest {
			t.Fatalf("ParsePositiveInt64(%q) = ok %v, status %d", value, ok, response.Code)
		}
	}
	response := httptest.NewRecorder()
	if value, ok := ParsePositiveInt64(response, " 42 ", "invalid id"); !ok || value != 42 {
		t.Fatalf("valid result = (%d, %v)", value, ok)
	}
}
