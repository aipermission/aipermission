package httptransport

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestBoundaryDownloadMethodWorkspaceMatrix(t *testing.T) {
	for _, path := range []string{
		"/api/backup/download", "/api/settings/diagnostics", "/api/backup/providers/3/records/9/download",
		"/api/file-transfers/4/download", "/api/file-transfer-batches/4/download", "/api/connector-targets/1/profiles/2/backup",
	} {
		t.Run(path, func(t *testing.T) { testDownloadWorkspaceMethods(t, path) })
	}
}

func TestBoundaryHeadPreservesOrdinaryReadsAndAuthenticationOrder(t *testing.T) {
	for _, test := range []struct {
		name, path              string
		unlocked, authenticated bool
		status, calls           int
	}{
		{"ordinary status", "/api/status", true, true, http.StatusOK, 1},
		{"missing session", "/api/backup/download", true, false, http.StatusUnauthorized, 0},
		{"locked database", "/api/backup/download", false, false, http.StatusLocked, 0},
		{"console attach", "/api/console/sessions/4/attach", true, true, http.StatusMethodNotAllowed, 0},
		{"maintenance attach", "/api/settings/maintenance-console/attach", true, true, http.StatusMethodNotAllowed, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			boundary := HTTPBoundary{
				Routes:    http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(http.StatusOK) }),
				Lifecycle: openLifecycle{}, IsUnlocked: func() bool { return test.unlocked },
				HasSession:   func(*http.Request) bool { return test.authenticated },
				RequiresCSRF: func(string, string) bool { return false }, CurrentWorkspace: func() string { return "workspace-b" },
			}
			request := httptest.NewRequest(http.MethodHead, test.path, nil)
			request.Header.Set(WorkspaceHeaderName, "workspace-b")
			response := httptest.NewRecorder()
			boundary.serveHTTP(response, request)
			if response.Code != test.status || calls != test.calls {
				t.Fatalf("HEAD status %d want %d, routes %d want %d", response.Code, test.status, calls, test.calls)
			}
		})
	}
}

func testDownloadWorkspaceMethods(t *testing.T, path string) {
	t.Helper()
	var exports atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+path, func(w http.ResponseWriter, _ *http.Request) {
		exports.Add(1)
		_, _ = w.Write([]byte("current workspace artifact"))
	})
	boundary := HTTPBoundary{
		Routes: mux, Lifecycle: openLifecycle{}, IsUnlocked: func() bool { return true },
		IsLocalRemoteAddr: func(string) bool { return true }, IsLocalhostHeader: func(string) bool { return true },
		HasSession: func(r *http.Request) bool {
			cookie, err := r.Cookie("aipermission_ui_session_3212")
			return err == nil && cookie.Value == "current-session"
		},
		RequiresCSRF: func(string, string) bool { return false }, CurrentWorkspace: func() string { return "workspace-b" },
	}
	server := httptest.NewServer(boundary.Handler())
	t.Cleanup(server.Close)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, binding := range []string{"", "workspace-a", "workspace-b"} {
			request, err := http.NewRequest(method, server.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.AddCookie(&http.Cookie{Name: "aipermission_ui_session_3212", Value: "current-session"})
			request.Header.Set(WorkspaceHeaderName, binding)
			before := exports.Load()
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			wantStatus, wantExports := http.StatusConflict, before
			if binding == "workspace-b" {
				wantStatus = http.StatusMethodNotAllowed
				if method == http.MethodGet {
					wantStatus, wantExports = http.StatusOK, before+1
				}
			}
			if response.StatusCode != wantStatus || exports.Load() != wantExports {
				t.Fatalf("%s binding %q: status %d want %d, exports %d want %d", method, binding, response.StatusCode, wantStatus, exports.Load(), wantExports)
			}
			if method == http.MethodHead && len(body) != 0 {
				t.Fatalf("HEAD returned body bytes: %q", body)
			}
			if wantStatus == http.StatusMethodNotAllowed && response.Header.Get("Allow") != http.MethodGet {
				t.Fatalf("HEAD did not report GET-only endpoint")
			}
		}
	}
}
