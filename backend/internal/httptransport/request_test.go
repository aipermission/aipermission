package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsUntrustedRequestShapes(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		limit       int64
		status      int
	}{
		{name: "wrong content type", contentType: "text/plain", body: `{}`, status: http.StatusBadRequest},
		{name: "unknown field", contentType: "application/json", body: `{"extra":true}`, status: http.StatusBadRequest},
		{name: "trailing value", contentType: "application/json", body: `{} {}`, status: http.StatusBadRequest},
		{name: "too large", contentType: "application/json", body: `{"name":"long"}`, limit: 4, status: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			var target struct {
				Name string `json:"name"`
			}
			if DecodeJSON(response, request, &target, test.limit) {
				t.Fatal("expected decode failure")
			}
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

func TestDecodeJSONAcceptsOneStrictObject(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok"}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()
	var target struct {
		Name string `json:"name"`
	}
	if !DecodeJSON(response, request, &target, 0) || target.Name != "ok" {
		t.Fatalf("decoded target = %#v, response = %s", target, response.Body.String())
	}
}
