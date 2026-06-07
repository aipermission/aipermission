package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/console"
)

func (s consoleHandlers) listConsoleSessions(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	serverID := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("server_id")); raw != "" {
		parsed, ok := parseInt64Query(w, raw, "server_id")
		if !ok {
			return
		}
		serverID = parsed
	}
	items, err := runtime.consoleSessions.List(r.Context(), serverID)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s consoleHandlers) createConsoleSession(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request console.CreateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	item, err := runtime.consoleSessions.Create(r.Context(), request)
	if errors.Is(err, console.ErrSessionLimit) {
		writeError(w, http.StatusConflict, err.Error())
		return
	} else if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, item.ServerID, "console.session.created", map[string]any{
		"session_id":     item.ID,
		"name":           item.Name,
		"close_existing": request.CloseExisting,
	})
	writeJSON(w, http.StatusCreated, item)
}

func (s consoleHandlers) getConsoleSession(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	item, err := runtime.consoleSessions.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "console session not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s consoleHandlers) inputConsoleSession(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request console.InputRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := runtime.consoleSessions.Input(r.Context(), id, request.Data); errors.Is(err, console.ErrInputTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	} else if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if serverID, err := consoleSessionServerID(r.Context(), runtime, id); err == nil {
		s.writeAudit(r.Context(), runtime, "user", nil, serverID, "console.session.input", map[string]any{
			"session_id": id,
			"bytes":      len(request.Data),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent"})
}

func (s consoleHandlers) closeConsoleSession(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := runtime.consoleSessions.Close(r.Context(), id); err != nil {
		writeInternalError(w)
		return
	}
	if err := s.cancelRunningCommandRequestsForSession(r.Context(), runtime, id, "console session closed before command completed"); err != nil {
		writeInternalError(w)
		return
	}
	if serverID, err := consoleSessionServerID(r.Context(), runtime, id); err == nil {
		s.writeAudit(r.Context(), runtime, "user", nil, serverID, "console.session.closed", map[string]any{
			"session_id": id,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "closed"})
}

func (s consoleHandlers) attachConsoleSession(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := runtime.consoleSessions.Attach(w, r, id, s.upgradeWebSocket); errors.Is(err, console.ErrNotFound) {
		writeError(w, http.StatusNotFound, "console session not found")
	} else if errors.Is(err, console.ErrClientLimit) {
		writeError(w, http.StatusConflict, err.Error())
	} else if err != nil {
		var inactive console.InactiveError
		if errors.As(err, &inactive) {
			writeError(w, http.StatusConflict, inactive.Error())
			return
		}
		writeInternalError(w)
	}
}
