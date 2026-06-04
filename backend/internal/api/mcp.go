package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

const (
	defaultMCPConsoleTailBytes = 20000
	maxMCPConsoleTailBytes     = 100000
)

func (s mcpHandlers) mcpListServers(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	settings, err := readSecuritySettings(r.Context(), auth.runtime)
	if err != nil {
		writeInternalError(w)
		return
	}

	rows, err := auth.runtime.database.QueryContext(r.Context(), `
		SELECT srv.id, srv.name, srv.description, srv.host, srv.port, srv.username, p.execution_rule
		FROM token_server_permissions p
		JOIN servers srv ON srv.id = p.server_id
		WHERE p.token_id = ? AND p.execution_rule != ?
		ORDER BY srv.name`,
		auth.TokenID,
		tokens.RuleBlocked,
	)
	if err != nil {
		writeInternalError(w)
		return
	}
	defer rows.Close()

	items := []mcpServerItem{}
	for rows.Next() {
		var item mcpServerItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Host, &item.Port, &item.Username, &item.ExecutionRule); err != nil {
			writeInternalError(w)
			return
		}
		if !settings.ExposeMCPServerMetadata {
			item.Host = ""
			item.Port = 0
			item.Username = ""
		}
		item.Hints = mcpServerHints(item.ExecutionRule)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w)
		return
	}

	writeJSON(w, http.StatusOK, items)
}

func (s mcpHandlers) mcpExec(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	if s.rejectStoppedMCP(w, auth.runtime) {
		return
	}

	var request mcpExecRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.Command = strings.TrimSpace(request.Command)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.ServerID < 1 {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	if request.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}
	if err := validateTextLimit("command", request.Command, maxCommandBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateTextLimit("reason", request.Reason, maxReasonBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	serverName, rule, allowed, err := s.mcpPermission(r.Context(), auth.runtime, auth.TokenID, request.ServerID)
	if err != nil {
		writeInternalError(w)
		return
	}
	if !allowed || rule == tokens.RuleBlocked {
		s.writeAudit(r.Context(), auth.runtime, "mcp", int64Ptr(auth.TokenID), request.ServerID, "mcp.exec.blocked", map[string]any{
			"command": request.Command,
			"reason":  request.Reason,
		})
		writeJSON(w, http.StatusOK, mcpExecResponse{
			Status:   "blocked",
			ServerID: request.ServerID,
			Command:  request.Command,
			Error:    "This token is blocked from executing commands on this server",
		})
		return
	}
	if rule == tokens.RuleApprovalRequired {
		id, err := s.insertCommandRequest(r.Context(), auth.runtime, auth.TokenID, request.ServerID, request.Command, request.Reason, "pending_approval")
		if err != nil {
			writeInternalError(w)
			return
		}
		s.writeAudit(r.Context(), auth.runtime, "mcp", int64Ptr(auth.TokenID), request.ServerID, "mcp.exec.approval_pending", map[string]any{
			"request_id": id,
			"command":    request.Command,
			"reason":     request.Reason,
		})
		userNote, _ := s.consumeNextUserMessage(r.Context(), auth.runtime, auth.TokenID, request.ServerID, 0)
		writeJSON(w, http.StatusOK, mcpExecResponse{
			Status:            "approval_pending",
			RequestID:         id,
			ServerID:          request.ServerID,
			ServerName:        serverName,
			Command:           request.Command,
			Error:             "Waiting for user approval. Use get_request to check this request until it reaches a terminal status.",
			UserNote:          userNote,
			RetryAfterSeconds: 3,
			AssistantHint:     pendingApprovalAssistantHint,
		})
		return
	}

	commandRequestID, err := s.insertCommandRequest(r.Context(), auth.runtime, auth.TokenID, request.ServerID, request.Command, request.Reason, "running")
	if err != nil {
		writeInternalError(w)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), mcpInitialExecTimeout)
	defer cancel()

	result, err := auth.runtime.consoleSessions.Exec(ctx, request.ServerID, request.Command)
	if err != nil {
		_ = s.finishCommandRequest(context.Background(), auth.runtime, commandRequestID, "error", 0, "", "", 0, err.Error())
		s.writeAudit(context.Background(), auth.runtime, "mcp", int64Ptr(auth.TokenID), request.ServerID, "mcp.exec.error", map[string]any{
			"request_id": commandRequestID,
			"command":    request.Command,
		})
		writeJSON(w, http.StatusOK, mcpExecResponse{
			Status:     "error",
			RequestID:  commandRequestID,
			ServerID:   request.ServerID,
			ServerName: serverName,
			Command:    request.Command,
			Error:      err.Error(),
		})
		return
	}

	if result.Running {
		_ = s.setCommandRequestSession(context.Background(), auth.runtime, commandRequestID, result.SessionID)
		s.writeAudit(context.Background(), auth.runtime, "mcp", int64Ptr(auth.TokenID), request.ServerID, "mcp.exec.running", map[string]any{
			"request_id": commandRequestID,
			"session_id": result.SessionID,
			"command":    request.Command,
			"reason":     request.Reason,
		})
		go s.finishActiveCommandRequest(auth.runtime, commandRequestID, request.ServerID)
		userNote, _ := s.consumeNextUserMessage(context.Background(), auth.runtime, auth.TokenID, request.ServerID, result.SessionID)
		writeJSON(w, http.StatusOK, mcpExecResponse{
			Status:            "running",
			RequestID:         commandRequestID,
			SessionID:         result.SessionID,
			ServerID:          request.ServerID,
			ServerName:        serverName,
			Command:           result.Command,
			Stdout:            s.redactForPersistence(context.Background(), auth.runtime, console.PlainOutput(result.Output)),
			DurationMS:        result.DurationMS,
			Error:             "Command is still running in the persistent console session. Use read_console to inspect live output before sending another command.",
			UserNote:          userNote,
			RetryAfterSeconds: 3,
			AssistantHint:     runningAssistantHint,
		})
		return
	}

	status := "completed"
	if result.ExitCode != 0 {
		status = "failed"
	}
	output := s.redactForPersistence(context.Background(), auth.runtime, console.PlainOutput(result.Output))
	_ = s.finishCommandRequest(context.Background(), auth.runtime, commandRequestID, status, result.SessionID, output, "", result.ExitCode, "")
	s.writeAudit(context.Background(), auth.runtime, "mcp", int64Ptr(auth.TokenID), request.ServerID, "mcp.exec."+status, map[string]any{
		"request_id":  commandRequestID,
		"session_id":  result.SessionID,
		"command":     request.Command,
		"exit_code":   result.ExitCode,
		"duration_ms": result.DurationMS,
	})
	userNote, _ := s.consumeNextUserMessage(context.Background(), auth.runtime, auth.TokenID, request.ServerID, result.SessionID)

	writeJSON(w, http.StatusOK, mcpExecResponse{
		Status:     status,
		RequestID:  commandRequestID,
		SessionID:  result.SessionID,
		ServerID:   request.ServerID,
		ServerName: serverName,
		Command:    request.Command,
		Stdout:     output,
		Stderr:     "",
		ExitCode:   result.ExitCode,
		DurationMS: result.DurationMS,
		UserNote:   userNote,
	})
}

func (s mcpHandlers) mcpListRequests(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	items, err := s.listCommandRequests(r.Context(), auth.runtime, commandRequestFilter{TokenID: auth.TokenID, Source: commandRequestSourceMCP, Status: status})
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s mcpHandlers) mcpGetRequest(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	item, err := s.getCommandRequest(r.Context(), auth.runtime, id, auth.TokenID, commandRequestSourceMCP)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	sessionID := int64(0)
	if item.SessionID != nil {
		sessionID = *item.SessionID
	}
	userNote, _ := s.consumeNextUserMessage(r.Context(), auth.runtime, auth.TokenID, item.ServerID, sessionID)
	if userNote != nil && item.UserNote == nil {
		item.UserNote = userNote
	}
	writeJSON(w, http.StatusOK, item)
}

func (s mcpHandlers) mcpReadConsole(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	if s.rejectStoppedMCP(w, auth.runtime) {
		return
	}

	serverID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("server_id")), 10, 64)
	if err != nil || serverID < 1 {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	tail := defaultMCPConsoleTailBytes
	if rawTail := strings.TrimSpace(r.URL.Query().Get("tail")); rawTail != "" {
		parsed, err := strconv.Atoi(rawTail)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "invalid tail")
			return
		}
		tail = parsed
	}
	if tail > maxMCPConsoleTailBytes {
		tail = maxMCPConsoleTailBytes
	}

	serverName, rule, allowed, err := s.mcpPermission(r.Context(), auth.runtime, auth.TokenID, serverID)
	if err != nil {
		writeInternalError(w)
		return
	}
	if !allowed || rule == tokens.RuleBlocked {
		writeJSON(w, http.StatusOK, mcpConsoleResponse{
			Status:   "blocked",
			ServerID: serverID,
			Error:    "This token is blocked from reading this server console",
		})
		return
	}
	if rule != tokens.RuleAlwaysRun {
		writeJSON(w, http.StatusOK, mcpConsoleResponse{
			Status:   "blocked",
			ServerID: serverID,
			Error:    "read_console requires always_run permission for this server; use get_request to inspect approval_required command results",
		})
		return
	}

	sessions, err := auth.runtime.consoleSessions.List(r.Context(), serverID)
	if err != nil {
		writeInternalError(w)
		return
	}
	if len(sessions) == 0 {
		userNote, _ := s.consumeNextUserMessage(r.Context(), auth.runtime, auth.TokenID, serverID, 0)
		writeJSON(w, http.StatusOK, mcpConsoleResponse{
			Status:     "none",
			ServerID:   serverID,
			ServerName: serverName,
			UserNote:   userNote,
		})
		return
	}

	session := sessions[0]
	transcript := session.Transcript
	transcript = console.TailStringByBytes(transcript, tail)
	transcript = s.redactForPersistence(r.Context(), auth.runtime, transcript)
	userNote, _ := s.consumeNextUserMessage(r.Context(), auth.runtime, auth.TokenID, serverID, session.ID)
	writeJSON(w, http.StatusOK, mcpConsoleResponse{
		Status:     session.Status,
		SessionID:  session.ID,
		ServerID:   serverID,
		ServerName: serverName,
		Transcript: console.PlainOutput(transcript),
		Error:      session.Error,
		UserNote:   userNote,
	})
}
