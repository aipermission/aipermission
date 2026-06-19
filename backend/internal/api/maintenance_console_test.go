package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestMaintenanceConsoleRoutesOpenRealtimeLocalPTY(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()

	statusResponse := performJSON(handler, http.MethodGet, "/api/settings/maintenance-console/status", "", nil)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("maintenance status failed: %d %s", statusResponse.Code, statusResponse.Body.String())
	}
	if !strings.Contains(statusResponse.Body.String(), `"scope":"local-ui-only"`) || !strings.Contains(statusResponse.Body.String(), `"mode":"realtime-pty"`) {
		t.Fatalf("unexpected maintenance status: %s", statusResponse.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/settings/maintenance-console/open", "", map[string]any{}); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"opened":true`) {
		t.Fatalf("maintenance open failed: %d %s", response.Code, response.Body.String())
	}
	statusAfterOpen := performJSON(handler, http.MethodGet, "/api/settings/maintenance-console/status", "", nil)
	if statusAfterOpen.Code != http.StatusOK || !strings.Contains(statusAfterOpen.Body.String(), `"status":"connected"`) {
		t.Fatalf("maintenance status should report connected: %d %s", statusAfterOpen.Code, statusAfterOpen.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/settings/maintenance-console/close", "", map[string]any{}); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"closed":true`) {
		t.Fatalf("maintenance close failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/api/audit-logs", "", nil); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), "maintenance_console.opened") ||
		!strings.Contains(response.Body.String(), "maintenance_console.closed") {
		t.Fatalf("maintenance console should audit terminal lifecycle: %d %s", response.Code, response.Body.String())
	}
}

func TestMaintenanceConsoleRequiresUnlockedDatabase(t *testing.T) {
	locked := NewLockedServer(fixtureConfigForLockedTest(t))
	if response := performJSON(locked.Handler(), http.MethodGet, "/api/settings/maintenance-console/status", "", nil); response.Code != http.StatusLocked {
		t.Fatalf("locked maintenance status should fail, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(locked.Handler(), http.MethodPost, "/api/settings/maintenance-console/open", "", map[string]any{}); response.Code != http.StatusLocked {
		t.Fatalf("locked maintenance open should fail, got %d %s", response.Code, response.Body.String())
	}
}
