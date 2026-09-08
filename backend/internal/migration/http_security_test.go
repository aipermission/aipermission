package migration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPBoundaryRejectsRemoteClientsAndHosts(t *testing.T) {
	server := newTestServer(t)

	tests := []struct {
		name       string
		host       string
		remoteAddr string
	}{
		{name: "remote host", host: "192.0.2.10:3211", remoteAddr: "127.0.0.1:12345"},
		{name: "remote client", host: "localhost:3211", remoteAddr: "192.0.2.20:12345"},
		{name: "missing host", remoteAddr: "127.0.0.1:12345"},
		{name: "missing remote address", host: "localhost:3211"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			request.Host = test.host
			request.RemoteAddr = test.remoteAddr
			response := httptest.NewRecorder()

			server.Handler().ServeHTTP(response, request)

			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
		})
	}
}

func TestHTTPBoundaryRequiresSameOriginAndCSRFForMigration(t *testing.T) {
	server := newTestServer(t)

	tests := []struct {
		name   string
		origin string
		csrf   string
	}{
		{name: "missing origin", csrf: server.csrfToken},
		{name: "remote origin", origin: "https://example.com", csrf: server.csrfToken},
		{name: "cross-site fetch", origin: "http://localhost:3211", csrf: server.csrfToken},
		{name: "missing csrf", origin: "http://localhost:3211"},
		{name: "invalid csrf", origin: "http://localhost:3211", csrf: "wrong-token"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/migrate", strings.NewReader(`{}`))
			request.Host = "localhost:3211"
			request.RemoteAddr = "127.0.0.1:12345"
			request.Header.Set("Origin", test.origin)
			request.Header.Set(csrfHeaderName, test.csrf)
			request.Header.Set("Content-Type", "application/json")
			if test.name == "cross-site fetch" {
				request.Header.Set("Sec-Fetch-Site", "cross-site")
			}
			response := httptest.NewRecorder()

			server.Handler().ServeHTTP(response, request)

			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
		})
	}
}

func TestHTTPBoundaryAllowsProtectedLocalMigrationRequest(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/migrate", strings.NewReader(`{}`))
	request.Host = "localhost:3211"
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Origin", "http://localhost:3211")
	request.Header.Set(csrfHeaderName, server.csrfToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code == http.StatusForbidden {
		t.Fatalf("protected local request was rejected: %s", response.Body.String())
	}
}

func TestHTTPBoundaryAllowsSameOriginRefererFallback(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/migrate", strings.NewReader(`{}`))
	request.Host = "localhost:3211"
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Referer", "http://localhost:3211/")
	request.Header.Set(csrfHeaderName, server.csrfToken)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code == http.StatusForbidden || response.Code == http.StatusUnsupportedMediaType {
		t.Fatalf("protected local referer request was rejected: %s", response.Body.String())
	}
}

func TestHTTPBoundaryRejectsUnexpectedMigrationContentType(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/migrate", strings.NewReader(`{}`))
	request.Host = "localhost:3211"
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Origin", "http://localhost:3211")
	request.Header.Set(csrfHeaderName, server.csrfToken)
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
}

func TestMigrationRequestRateLimitIsBounded(t *testing.T) {
	server := newTestServer(t)
	for attempt := 0; attempt < 10; attempt++ {
		if !server.requestLimiter.Allow("migration") {
			t.Fatalf("attempt %d was unexpectedly limited", attempt+1)
		}
	}
	if server.requestLimiter.Allow("migration") {
		t.Fatal("request beyond migration rate limit was accepted")
	}
}

func TestHTTPBoundarySetsBrowserSecurityHeaders(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "localhost:3211"
	request.RemoteAddr = "127.0.0.1:12345"
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	for name, want := range map[string]string{
		"Cache-Control":          "no-store, private",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if csp := response.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "script-src 'nonce-") {
		t.Fatalf("unexpected content security policy: %q", csp)
	}
	if body := response.Body.String(); !strings.Contains(body, `name="csrf-token"`) || !strings.Contains(body, `nonce="`) {
		t.Fatalf("migration page does not carry CSRF/CSP tokens")
	}
}

func TestMigrationConcurrencyGuardRejectsSecondRequest(t *testing.T) {
	server := newTestServer(t)
	if !server.acquireMigrationSlot() {
		t.Fatal("first migration slot was rejected")
	}
	defer server.releaseMigrationSlot()
	if server.acquireMigrationSlot() {
		t.Fatal("second migration slot was accepted")
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	server, err := NewServer(Config{DataPath: t.TempDir(), GatewaySecret: "test-secret"})
	if err != nil {
		t.Fatalf("create migration server: %v", err)
	}
	return server
}
