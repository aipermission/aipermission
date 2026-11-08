package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCapacitySocketReleasesWorkspaceLeaseAfterUpgrade(t *testing.T) {
	lifecycle := &trackedLifecycle{}
	request := httptest.NewRequest(http.MethodGet, "/api/console/capacity/attach?workspace=workspace-a", nil)
	response := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}
	defer func() {
		if response.peer != nil {
			_ = response.peer.Close()
		}
	}()
	HTTPBoundary{
		Routes: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if lifecycle.released {
				t.Fatal("capacity upgrade was not fenced")
			}
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			if !lifecycle.released {
				t.Fatal("capacity watcher blocks workspace lock/switch")
			}
		}),
		Lifecycle: lifecycle, IsUnlocked: func() bool { return true }, HasSession: func(*http.Request) bool { return true },
		RequiresCSRF: func(string, string) bool { return false }, CurrentWorkspace: func() string { return "workspace-a" },
	}.serveHTTP(response, request)
}
