package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/config"
)

func TestHTTPResponsePolicyCoversRouteClassesAndBoundaryFailures(t *testing.T) {
	server := NewLockedServer(config.Config{
		Host: "127.0.0.1", Port: "8080", DataPath: t.TempDir() + "/aipermission.db", GatewaySecret: "test-secret",
	})
	tests := []struct {
		name       string
		path       string
		host       string
		wantStatus int
	}{
		{name: "health", path: "/health", host: "localhost:8080", wantStatus: http.StatusOK},
		{name: "unlock API", path: "/api/unlock/status", host: "localhost:8080", wantStatus: http.StatusOK},
		{name: "unknown API route", path: "/api/not-found", host: "localhost:8080", wantStatus: http.StatusLocked},
		{name: "host rejection", path: "/health", host: "example.test", wantStatus: http.StatusForbidden},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Host = test.host
			request.RemoteAddr = "127.0.0.1:12345"
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			assertNoStoreResponseHeaders(t, response)
			if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
			}
		})
	}
}

func assertNoStoreResponseHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("Cache-Control"); got != "no-store, private" {
		t.Fatalf("Cache-Control = %q, want no-store, private", got)
	}
	if got := response.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", got)
	}
}
