package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestMaintenanceConsoleRoutesRunBoundedLocalCommands(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()

	statusResponse := performJSON(handler, http.MethodGet, "/api/settings/maintenance-console/status", "", nil)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("maintenance status failed: %d %s", statusResponse.Code, statusResponse.Body.String())
	}
	if !strings.Contains(statusResponse.Body.String(), `"scope":"local-ui-only"`) || !strings.Contains(statusResponse.Body.String(), `"max_timeout_sec":30`) {
		t.Fatalf("unexpected maintenance status: %s", statusResponse.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/settings/maintenance-console/open", "", map[string]any{}); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"opened":true`) {
		t.Fatalf("maintenance open failed: %d %s", response.Code, response.Body.String())
	}

	runResponse := performJSON(handler, http.MethodPost, "/api/settings/maintenance-console/run", "", map[string]any{
		"command":         "printf ready",
		"timeout_seconds": 2,
	})
	if runResponse.Code != http.StatusOK {
		t.Fatalf("maintenance run failed: %d %s", runResponse.Code, runResponse.Body.String())
	}
	body := runResponse.Body.String()
	for _, want := range []string{`"status":"completed"`, `"stdout":"ready"`, `"exit_code":0`, `"timed_out":false`} {
		if !strings.Contains(body, want) {
			t.Fatalf("maintenance run missing %s: %s", want, body)
		}
	}

	failureResponse := performJSON(handler, http.MethodPost, "/api/settings/maintenance-console/run", "", map[string]any{
		"command": "printf problem >&2; exit 7",
	})
	if failureResponse.Code != http.StatusOK {
		t.Fatalf("maintenance failure should return command result: %d %s", failureResponse.Code, failureResponse.Body.String())
	}
	failureBody := failureResponse.Body.String()
	for _, want := range []string{`"status":"failed"`, `"stderr":"problem"`, `"exit_code":7`} {
		if !strings.Contains(failureBody, want) {
			t.Fatalf("maintenance failure missing %s: %s", want, failureBody)
		}
	}

	timeoutResponse := performJSON(handler, http.MethodPost, "/api/settings/maintenance-console/run", "", map[string]any{
		"command":         "sleep 2",
		"timeout_seconds": 1,
	})
	if timeoutResponse.Code != http.StatusOK || !strings.Contains(timeoutResponse.Body.String(), `"status":"timed_out"`) {
		t.Fatalf("maintenance timeout failed: %d %s", timeoutResponse.Code, timeoutResponse.Body.String())
	}

	if response := performJSON(handler, http.MethodPost, "/api/settings/maintenance-console/run", "", map[string]any{"command": "printf too slow", "timeout_seconds": 31}); response.Code != http.StatusBadRequest {
		t.Fatalf("large timeout should fail, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/settings/maintenance-console/close", "", map[string]any{}); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"closed":true`) {
		t.Fatalf("maintenance close failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/api/audit-logs", "", nil); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), "maintenance_console.opened") ||
		!strings.Contains(response.Body.String(), "maintenance_console.command.started") ||
		!strings.Contains(response.Body.String(), "maintenance_console.command.finished") ||
		!strings.Contains(response.Body.String(), "maintenance_console.closed") {
		t.Fatalf("maintenance console should audit command lifecycle: %d %s", response.Code, response.Body.String())
	}
}

func TestMaintenanceConsoleRequiresUnlockedDatabase(t *testing.T) {
	locked := NewLockedServer(fixtureConfigForLockedTest(t))
	if response := performJSON(locked.Handler(), http.MethodGet, "/api/settings/maintenance-console/status", "", nil); response.Code != http.StatusLocked {
		t.Fatalf("locked maintenance status should fail, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(locked.Handler(), http.MethodPost, "/api/settings/maintenance-console/run", "", map[string]any{"command": "true"}); response.Code != http.StatusLocked {
		t.Fatalf("locked maintenance run should fail, got %d %s", response.Code, response.Body.String())
	}
}
