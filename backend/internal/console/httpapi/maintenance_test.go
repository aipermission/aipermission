package consolehttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/gorilla/websocket"
)

type fakeMaintenanceRuntime struct {
	descriptor console.MaintenanceConsoleDescriptor
	snapshot   console.MaintenanceConsoleSnapshot
	active     bool
	closed     bool
}

func (r *fakeMaintenanceRuntime) Descriptor() console.MaintenanceConsoleDescriptor {
	return r.descriptor
}
func (r *fakeMaintenanceRuntime) Snapshot() (console.MaintenanceConsoleSnapshot, bool) {
	return r.snapshot, r.active
}
func (r *fakeMaintenanceRuntime) Open() (console.MaintenanceConsoleSnapshot, error) {
	r.active = true
	return r.snapshot, nil
}
func (r *fakeMaintenanceRuntime) Active() bool           { return r.active }
func (r *fakeMaintenanceRuntime) Attach(*websocket.Conn) {}
func (r *fakeMaintenanceRuntime) Close() bool {
	closed := r.active
	r.active = false
	r.closed = closed
	return closed
}

func TestMaintenanceHTTPHandlersOwnLifecycleResponses(t *testing.T) {
	runtime := &fakeMaintenanceRuntime{
		descriptor: console.MaintenanceConsoleDescriptor{Supported: true, Shell: "/bin/sh", MaxInputBytes: 1024, MaxTranscriptBytes: 2048},
		snapshot:   console.MaintenanceConsoleSnapshot{Status: "connected", Shell: "/bin/sh"},
	}
	observed := make([]string, 0, 2)
	handlers := NewMaintenanceHTTPHandlers(func(http.ResponseWriter) (MaintenanceHTTPScope, bool) {
		return MaintenanceHTTPScope{
			Runtime: runtime,
			Observe: func(_ context.Context, action string, _ map[string]any) { observed = append(observed, action) },
		}, true
	})

	status := httptest.NewRecorder()
	handlers.Status(status, httptest.NewRequest(http.MethodGet, "/status", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"enabled":true`) {
		t.Fatalf("status response = %d %s", status.Code, status.Body.String())
	}
	opened := httptest.NewRecorder()
	handlers.Open(opened, httptest.NewRequest(http.MethodPost, "/open", nil))
	if opened.Code != http.StatusOK || !runtime.active || len(observed) != 1 || observed[0] != "maintenance_console.opened" {
		t.Fatalf("open response = %d %s active=%v observed=%v", opened.Code, opened.Body.String(), runtime.active, observed)
	}
	closed := httptest.NewRecorder()
	handlers.Close(closed, httptest.NewRequest(http.MethodPost, "/close", nil))
	if closed.Code != http.StatusOK || !runtime.closed || len(observed) != 2 || observed[1] != "maintenance_console.closed" {
		t.Fatalf("close response = %d %s closed=%v observed=%v", closed.Code, closed.Body.String(), runtime.closed, observed)
	}
}

func TestMaintenanceHTTPHandlersUseRequirementSpecificPorts(t *testing.T) {
	handlers := NewMaintenanceHTTPHandlers(func(http.ResponseWriter) (MaintenanceHTTPScope, bool) {
		return MaintenanceHTTPScope{}, true
	})
	status := httptest.NewRecorder()
	handlers.Status(status, httptest.NewRequest(http.MethodGet, "/status", nil))
	if status.Code != http.StatusOK {
		t.Fatalf("status without mutation ports = %d %s", status.Code, status.Body.String())
	}
	opened := httptest.NewRecorder()
	handlers.Open(opened, httptest.NewRequest(http.MethodPost, "/open", nil))
	if opened.Code != http.StatusInternalServerError {
		t.Fatalf("open without observation port = %d %s", opened.Code, opened.Body.String())
	}
}
