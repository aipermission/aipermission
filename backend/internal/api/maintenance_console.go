package api

import (
	"context"
	"net/http"
	"time"
)

func (h maintenanceConsoleHandlers) status(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.activeRuntimeOrLocked(w); !ok {
		return
	}
	sessionStatus := "closed"
	shell := ""
	enabled := false
	maxInputBytes := 0
	maxTranscriptBytes := 0
	if h.maintenanceConsole != nil {
		descriptor := h.maintenanceConsole.Descriptor()
		enabled = descriptor.Supported
		shell = descriptor.Shell
		maxInputBytes = descriptor.MaxInputBytes
		maxTranscriptBytes = descriptor.MaxTranscriptBytes
		if snapshot, ok := h.maintenanceConsole.Snapshot(); ok {
			sessionStatus = snapshot.Status
			shell = snapshot.Shell
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":              enabled,
		"scope":                "local-ui-only",
		"mode":                 "realtime-pty",
		"shell":                shell,
		"status":               sessionStatus,
		"max_input_bytes":      maxInputBytes,
		"max_transcript_bytes": maxTranscriptBytes,
	})
}

func (h maintenanceConsoleHandlers) open(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	if h.maintenanceConsole == nil || !h.maintenanceConsole.Descriptor().Supported {
		writeError(w, http.StatusNotImplemented, "maintenance console is not supported on this platform")
		return
	}
	snapshot, err := h.maintenanceConsole.Open()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "maintenance console failed to start")
		return
	}
	h.writeObservationAudit(r.Context(), runtime, "user", nil, 0, "maintenance_console.opened", map[string]any{
		"scope": "local-ui-only",
		"mode":  "realtime-pty",
		"shell": snapshot.Shell,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"opened":    true,
		"mode":      "realtime-pty",
		"shell":     snapshot.Shell,
		"status":    snapshot.Status,
		"opened_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h maintenanceConsoleHandlers) close(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	closed := false
	if h.maintenanceConsole != nil {
		closed = h.maintenanceConsole.Close()
	}
	h.writeObservationAudit(r.Context(), runtime, "user", nil, 0, "maintenance_console.closed", map[string]any{
		"scope":  "local-ui-only",
		"mode":   "realtime-pty",
		"closed": closed,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"closed":    true,
		"closed_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h maintenanceConsoleHandlers) attach(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.activeRuntimeOrLocked(w); !ok {
		return
	}
	if h.maintenanceConsole == nil || !h.maintenanceConsole.Descriptor().Supported {
		writeError(w, http.StatusNotImplemented, "maintenance console is not supported on this platform")
		return
	}
	if !h.maintenanceConsole.Active() {
		writeError(w, http.StatusConflict, "maintenance console is not open")
		return
	}
	ws, err := h.upgradeWebSocket(w, r)
	if err != nil {
		return
	}
	h.maintenanceConsole.Attach(ws)
}

func (s *Server) closeMaintenanceConsoleForLifecycle(reason string) bool {
	if s == nil || s.maintenanceConsole == nil {
		return false
	}
	if !s.maintenanceConsole.Close() {
		return false
	}
	runtime := s.activeRuntime()
	if runtime != nil {
		s.writeObservationAudit(context.Background(), runtime, "system", nil, 0, "maintenance_console.closed", map[string]any{
			"scope":  "local-ui-only",
			"mode":   "realtime-pty",
			"closed": true,
			"reason": reason,
		})
	}
	return true
}
