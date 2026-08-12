package api

import (
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type mcpRuntimeResponse struct {
	Enabled      bool   `json:"enabled"`
	StartEnabled bool   `json:"start_enabled"`
	UpdatedAt    string `json:"updated_at"`
}

type updateMCPRuntimeRequest struct {
	Enabled bool `json:"enabled"`
}

func (s mcpHandlers) getMCPRuntime(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	settings, err := readSecuritySettings(r.Context(), runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, mcpRuntimeResponse{
		Enabled:      runtime.isMCPStarted(),
		StartEnabled: settings.MCPStartEnabled,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s mcpHandlers) updateMCPRuntime(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request updateMCPRuntimeRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	var release func()
	if !request.Enabled {
		var err error
		release, err = runtime.vaultDelivery.acquire(r.Context())
		if err != nil {
			writeError(w, http.StatusRequestTimeout, "MCP stop was canceled")
			return
		}
		defer release()
	}
	runtime.setMCPStarted(request.Enabled)
	if !request.Enabled {
		runtimeIDs, err := vaultAllRuntimeIDs(r.Context(), runtime)
		if err != nil {
			writeInternalError(w)
			return
		}
		runtime.vaultLeases.Clear()
		if err := revokeAllPersistedVaultLeases(r.Context(), runtime); err != nil {
			writeInternalError(w)
			return
		}
		if err := s.invalidateVaultRuntimeSessions(
			r.Context(),
			runtime,
			runtimeIDs,
			"MCP execution stopped; send a fresh Vault request after it starts",
		); err != nil {
			writeInternalError(w)
			return
		}
		store := s.vaultRequestStore(r.Context(), runtime)
		if err := store.StalePendingForAction(
			r.Context(),
			vaultrequests.ActionGenerateItem,
			"MCP execution stopped; send a fresh Vault request after it starts",
		); err != nil {
			writeInternalError(w)
			return
		}
		if err := store.FailRunning(r.Context(), "MCP execution stopped while the Vault action was running"); err != nil {
			writeInternalError(w)
			return
		}
	}
	action := "mcp.runtime.stopped"
	if request.Enabled {
		action = "mcp.runtime.started"
	}
	s.writeObservationAudit(r.Context(), runtime, "user", nil, 0, action, map[string]any{
		"enabled": request.Enabled,
	})
	settings, err := readSecuritySettings(r.Context(), runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, mcpRuntimeResponse{
		Enabled:      runtime.isMCPStarted(),
		StartEnabled: settings.MCPStartEnabled,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	})
}

func (runtime *databaseRuntime) isMCPStarted() bool {
	runtime.mcpMu.RLock()
	defer runtime.mcpMu.RUnlock()
	return runtime.mcpStarted
}

func (runtime *databaseRuntime) setMCPStarted(enabled bool) {
	runtime.mcpMu.Lock()
	runtime.mcpStarted = enabled
	runtime.mcpMu.Unlock()
}

func (s *Server) rejectStoppedMCP(w http.ResponseWriter, runtime *databaseRuntime) bool {
	if runtime.isMCPStarted() {
		return false
	}
	writeStoppedMCP(w)
	return true
}

func writeStoppedMCP(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "stopped",
		"error":  "MCP execution is stopped in the local gateway. Start MCP from the web UI before running commands.",
	})
}
