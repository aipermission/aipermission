package api

import (
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestAttachmentHeadersRejectFilenameInjectionAndSniffing(t *testing.T) {
	response := httptest.NewRecorder()
	setAttachmentHeaders(response, "../report\r\nX-Injected: yes.json", "application/json")

	assertNoStoreResponseHeaders(t, response)
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	disposition, parameters, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
	if err != nil {
		t.Fatalf("parse Content-Disposition: %v", err)
	}
	if disposition != "attachment" || parameters["filename"] != "reportX-Injected: yes.json" {
		t.Fatalf("unexpected Content-Disposition: %q %#v", disposition, parameters)
	}
	for name := range response.Header() {
		if strings.EqualFold(name, "X-Injected") {
			t.Fatalf("filename created an injected response header")
		}
	}
}

func TestSafeAttachmentFilenameUsesBoundedBasename(t *testing.T) {
	tests := map[string]string{
		"../nested/report.sql":       "report.sql",
		`..\nested\windows.sql`:      "windows.sql",
		"\x00\r\n":                   "fallback.bin",
		" normal-backup.aipdb ":      "normal-backup.aipdb",
		"folder/control\x00name.zip": "controlname.zip",
		"":                           "fallback.bin",
	}
	for input, want := range tests {
		if got := safeAttachmentFilename(input, "fallback.bin"); got != want {
			t.Fatalf("safeAttachmentFilename(%q) = %q, want %q", input, got, want)
		}
	}
	longName := strings.Repeat("x", maxAttachmentFilenameRunes+20)
	if got := safeAttachmentFilename(longName, "fallback.bin"); len([]rune(got)) != maxAttachmentFilenameRunes {
		t.Fatalf("long filename length = %d, want %d", len([]rune(got)), maxAttachmentFilenameRunes)
	}
	if got := safeAttachmentFilename("\x00", "../fallback\r\n.bin"); got != "fallback.bin" {
		t.Fatalf("sanitized fallback = %q, want fallback.bin", got)
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
