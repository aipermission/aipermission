package api

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/actions"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

const mcpBulkExecAssistantHint = "Each target has its own request_id when execution or approval started. Poll get_request(request_id) for running or approval_pending items. Approval-required targets wait for the local operator; blocked targets were skipped."

type mcpBulkExecRunItem struct {
	RequestID  int64
	ServerID   int64
	ServerName string
	Command    string
	Reason     string
	TokenID    int64
}

func (s mcpHandlers) mcpBulkExec(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	if s.rejectStoppedMCP(w, auth.runtime) {
		return
	}

	var request mcpBulkExecRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.Command = strings.TrimSpace(request.Command)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}
	if request.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required for bulk_exec")
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
	policyWarnings := analyzeCommandPolicy(request.Command)
	serverIDs, err := normalizeBulkConsoleServerIDs(request.ServerIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	items := make([]mcpBulkExecResponseItem, 0, len(serverIDs))
	runItems := []mcpBulkExecRunItem{}
	openItems := 0
	for _, serverID := range serverIDs {
		serverName, rule, allowed, err := s.mcpPermission(r.Context(), auth.runtime, auth.TokenID, serverID)
		if err != nil {
			writeInternalError(w)
			return
		}
		item := mcpBulkExecResponseItem{
			ServerID:      serverID,
			ServerName:    serverName,
			ExecutionRule: rule,
		}
		if !allowed || rule == tokens.RuleBlocked {
			item.Status = "blocked"
			item.Error = "This token is blocked from executing commands on this server"
			s.writeAudit(r.Context(), auth.runtime, "mcp", int64Ptr(auth.TokenID), serverID, "mcp.bulk_exec.blocked", map[string]any{
				"command": request.Command,
				"reason":  request.Reason,
			})
			items = append(items, item)
			continue
		}
		command := request.Command
		prepared, err := auth.runtime.prepareSSHConnectorAction(r.Context(), serverID, actions.PrepareRequest{
			Source:     commandRequestSourceMCP,
			ActionName: sshconnector.ActionExec,
			Input:      map[string]any{"command": command},
			Reason:     request.Reason,
		})
		if err != nil {
			writeInternalError(w)
			return
		}
		if preparedCommand, ok := prepared.Action.Payload["command"].(string); ok {
			command = preparedCommand
		}

		status := "running"
		if rule == tokens.RuleApprovalRequired {
			status = "pending_approval"
		}
		requestID, err := s.insertCommandRequest(r.Context(), auth.runtime, auth.TokenID, serverID, command, request.Reason, status)
		if err != nil {
			writeInternalError(w)
			return
		}
		item.RequestID = requestID
		item.Status = status
		item.ApprovalContextHash = s.commandRequestApprovalContextHash(r.Context(), auth.runtime, requestID)
		items = append(items, item)
		openItems++

		if rule == tokens.RuleApprovalRequired {
			s.writeAudit(r.Context(), auth.runtime, "mcp", int64Ptr(auth.TokenID), serverID, "mcp.bulk_exec.approval_pending", map[string]any{
				"request_id":            requestID,
				"command":               command,
				"reason":                request.Reason,
				"approval_context_hash": item.ApprovalContextHash,
			})
			continue
		}

		s.writeAudit(r.Context(), auth.runtime, "mcp", int64Ptr(auth.TokenID), serverID, "mcp.bulk_exec.running", map[string]any{
			"request_id": requestID,
			"command":    command,
			"reason":     request.Reason,
		})
		runItems = append(runItems, mcpBulkExecRunItem{
			RequestID:  requestID,
			ServerID:   serverID,
			ServerName: serverName,
			Command:    command,
			Reason:     request.Reason,
			TokenID:    auth.TokenID,
		})
	}

	if len(runItems) > 0 {
		s.runMCPBulkExecCommands(auth.runtime, runItems)
	}

	response := mcpBulkExecResponse{
		Status:         "accepted",
		Command:        request.Command,
		Parallelism:    bulkConsoleCommandParallelism,
		Items:          items,
		PolicyWarnings: policyWarnings,
	}
	if openItems > 0 {
		response.RetryAfterSeconds = 3
		response.AssistantHint = mcpBulkExecAssistantHint
	} else {
		response.Status = "blocked"
		response.AssistantHint = "No command was started because every requested target was blocked or unauthorized."
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (s *Server) runMCPBulkExecCommands(runtime *databaseRuntime, items []mcpBulkExecRunItem) {
	go func() {
		sem := make(chan struct{}, bulkConsoleCommandParallelism)
		var wg sync.WaitGroup
		for _, item := range items {
			item := item
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				s.runMCPBulkExecCommand(runtime, item)
			}()
		}
		wg.Wait()
	}()
}

func (s *Server) runMCPBulkExecCommand(runtime *databaseRuntime, item mcpBulkExecRunItem) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpInitialExecTimeout)
	defer cancel()

	result, err := runtime.consoleSessions.Exec(ctx, item.ServerID, item.Command)
	if err != nil {
		message := sshCommandFailureMessage(err)
		_ = s.finishCommandRequest(context.Background(), runtime, item.RequestID, "error", 0, "", "", 0, message)
		s.writeAudit(context.Background(), runtime, "mcp", int64Ptr(item.TokenID), item.ServerID, "mcp.bulk_exec.error", map[string]any{
			"request_id": item.RequestID,
			"command":    item.Command,
			"error":      message,
		})
		return
	}
	if result.Running {
		_ = s.setCommandRequestSession(context.Background(), runtime, item.RequestID, result.SessionID)
		s.writeAudit(context.Background(), runtime, "mcp", int64Ptr(item.TokenID), item.ServerID, "mcp.bulk_exec.running_background", map[string]any{
			"request_id": item.RequestID,
			"session_id": result.SessionID,
			"command":    item.Command,
			"reason":     item.Reason,
		})
		s.finishActiveCommandRequest(runtime, item.RequestID, item.ServerID)
		return
	}
	status := "completed"
	if result.ExitCode != 0 {
		status = "failed"
	}
	_ = s.finishCommandRequest(context.Background(), runtime, item.RequestID, status, result.SessionID, result.Output, "", result.ExitCode, "")
	s.writeAudit(context.Background(), runtime, "mcp", int64Ptr(item.TokenID), item.ServerID, "mcp.bulk_exec."+status, map[string]any{
		"request_id":  item.RequestID,
		"session_id":  result.SessionID,
		"command":     item.Command,
		"exit_code":   result.ExitCode,
		"duration_ms": result.DurationMS,
	})
}
