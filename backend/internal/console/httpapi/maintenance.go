package consolehttp

import (
	"context"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/gorilla/websocket"
)

type MaintenanceHTTPScope struct {
	Runtime          console.MaintenanceConsoleRuntime
	Observe          func(context.Context, string, map[string]any)
	UpgradeWebSocket func(http.ResponseWriter, *http.Request) (*websocket.Conn, error)
}

type MaintenanceHTTPScopeProvider func(http.ResponseWriter) (MaintenanceHTTPScope, bool)

type MaintenanceHTTPHandlers struct{ scope MaintenanceHTTPScopeProvider }

type maintenanceHTTPRequirement uint8

const (
	requireMaintenanceObserve maintenanceHTTPRequirement = 1 << iota
	requireMaintenanceUpgrade
)

func NewMaintenanceHTTPHandlers(scope MaintenanceHTTPScopeProvider) *MaintenanceHTTPHandlers {
	return &MaintenanceHTTPHandlers{scope: scope}
}

func (h *MaintenanceHTTPHandlers) Status(w http.ResponseWriter, _ *http.Request) {
	scope, ok := h.resolve(w, 0)
	if !ok {
		return
	}
	status := "closed"
	shell := ""
	enabled := false
	maxInputBytes := 0
	maxTranscriptBytes := 0
	if scope.Runtime != nil {
		descriptor := scope.Runtime.Descriptor()
		enabled = descriptor.Supported
		shell = descriptor.Shell
		maxInputBytes = descriptor.MaxInputBytes
		maxTranscriptBytes = descriptor.MaxTranscriptBytes
		if snapshot, exists := scope.Runtime.Snapshot(); exists {
			status = snapshot.Status
			shell = snapshot.Shell
		}
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"enabled": enabled, "scope": "local-ui-only", "mode": "realtime-pty",
		"shell": shell, "status": status, "max_input_bytes": maxInputBytes,
		"max_transcript_bytes": maxTranscriptBytes,
	})
}

func (h *MaintenanceHTTPHandlers) Open(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireMaintenanceObserve)
	if !ok {
		return
	}
	if scope.Runtime == nil || !scope.Runtime.Descriptor().Supported {
		httptransport.WriteError(w, http.StatusNotImplemented, "maintenance console is not supported on this platform")
		return
	}
	snapshot, err := scope.Runtime.Open()
	if err != nil {
		httptransport.WriteError(w, http.StatusInternalServerError, "maintenance console failed to start")
		return
	}
	scope.Observe(r.Context(), "maintenance_console.opened", map[string]any{
		"scope": "local-ui-only", "mode": "realtime-pty", "shell": snapshot.Shell,
	})
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"opened": true, "mode": "realtime-pty", "shell": snapshot.Shell,
		"status": snapshot.Status, "opened_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *MaintenanceHTTPHandlers) Close(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireMaintenanceObserve)
	if !ok {
		return
	}
	closed := false
	if scope.Runtime != nil {
		closed = scope.Runtime.Close()
	}
	scope.Observe(r.Context(), "maintenance_console.closed", map[string]any{
		"scope": "local-ui-only", "mode": "realtime-pty", "closed": closed,
	})
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"closed": true, "closed_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *MaintenanceHTTPHandlers) Attach(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireMaintenanceUpgrade)
	if !ok {
		return
	}
	if scope.Runtime == nil || !scope.Runtime.Descriptor().Supported {
		httptransport.WriteError(w, http.StatusNotImplemented, "maintenance console is not supported on this platform")
		return
	}
	if !scope.Runtime.Active() {
		httptransport.WriteError(w, http.StatusConflict, "maintenance console is not open")
		return
	}
	ws, err := scope.UpgradeWebSocket(w, r)
	if err != nil {
		return
	}
	scope.Runtime.Attach(ws)
}

func (h *MaintenanceHTTPHandlers) resolve(w http.ResponseWriter, requirement maintenanceHTTPRequirement) (MaintenanceHTTPScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return MaintenanceHTTPScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return MaintenanceHTTPScope{}, false
	}
	if requirement&requireMaintenanceObserve != 0 && scope.Observe == nil ||
		requirement&requireMaintenanceUpgrade != 0 && scope.UpgradeWebSocket == nil {
		httptransport.WriteInternalError(w)
		return MaintenanceHTTPScope{}, false
	}
	return scope, true
}
