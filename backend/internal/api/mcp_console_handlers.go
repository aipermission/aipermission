package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/tokens"
)

type consoleRestartResult struct {
	ClosedSessionIDs        []int64
	CanceledRunningRequests int64
}

func (s mcpHandlers) mcpRestartConsoleSession(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	if s.rejectStoppedMCP(w, auth.runtime) {
		return
	}

	var request struct {
		ServerID int64 `json:"server_id"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if request.ServerID < 1 {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return
	}

	serverName, rule, allowed, err := s.mcpPermission(r.Context(), auth.runtime, auth.TokenID, request.ServerID)
	if err != nil {
		writeInternalError(w)
		return
	}
	if !allowed || rule == tokens.RuleBlocked {
		writeJSON(w, http.StatusOK, mcpRestartConsoleResponse{
			Status:   "blocked",
			ServerID: request.ServerID,
			Error:    "This token is blocked from restarting this server console session",
		})
		return
	}

	result, err := s.restartServerConsoleSession(r.Context(), auth.runtime, request.ServerID, "console session restarted before command completed")
	if err != nil {
		writeInternalError(w)
		return
	}

	s.writeAudit(r.Context(), auth.runtime, "mcp", int64Ptr(auth.TokenID), request.ServerID, "mcp.console.restarted", map[string]any{
		"closed_session_ids":        result.ClosedSessionIDs,
		"canceled_running_requests": result.CanceledRunningRequests,
	})
	writeJSON(w, http.StatusOK, mcpRestartConsoleResponse{
		Status:                  "restarted",
		ServerID:                request.ServerID,
		ServerName:              serverName,
		ClosedSessionIDs:        result.ClosedSessionIDs,
		CanceledRunningRequests: result.CanceledRunningRequests,
		AssistantHint:           "The persistent console session was closed. The next exec call for this server will open a fresh SSH session.",
	})
}

func (s *Server) restartServerConsoleSession(ctx context.Context, runtime *databaseRuntime, serverID int64, runningRequestError string) (consoleRestartResult, error) {
	sessions, err := runtime.consoleSessions.List(ctx, serverID)
	if err != nil {
		return consoleRestartResult{}, err
	}
	closedSessionIDs := []int64{}
	for _, session := range sessions {
		if session.Status == "connecting" || session.Status == "connected" {
			closedSessionIDs = append(closedSessionIDs, session.ID)
		}
	}

	canceledRequests, err := s.cancelRunningCommandRequestsForServer(ctx, runtime, serverID, runningRequestError)
	if err != nil {
		return consoleRestartResult{}, err
	}
	if err := runtime.consoleSessions.CloseServer(ctx, serverID); err != nil {
		return consoleRestartResult{}, err
	}
	return consoleRestartResult{
		ClosedSessionIDs:        closedSessionIDs,
		CanceledRunningRequests: canceledRequests,
	}, nil
}
