package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/console"
)

const (
	bulkConsoleCommandMaxServers  = 25
	bulkConsoleCommandParallelism = 3
	bulkConsoleCommandReason      = "bulk console command"
)

type bulkConsoleCommandRequest struct {
	TargetIDs    []int64 `json:"target_ids"`
	Command      string  `json:"command"`
	Reason       string  `json:"reason"`
	Confirmation string  `json:"confirmation"`
}

type bulkConsoleCommandResponse struct {
	Parallelism int                              `json:"parallelism"`
	Items       []bulkConsoleCommandResponseItem `json:"items"`
}

type bulkConsoleCommandResponseItem struct {
	RequestID  int64  `json:"request_id"`
	TargetID   int64  `json:"target_id"`
	TargetName string `json:"target_name"`
	Status     string `json:"status"`
}

func (s consoleHandlers) runBulkConsoleCommand(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request bulkConsoleCommandRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.Command = strings.TrimSpace(request.Command)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" {
		request.Reason = bulkConsoleCommandReason
	}
	if err := validateTextLimit("command", request.Command, maxCommandBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateTextLimit("reason", request.Reason, maxReasonBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	targetIDs, err := normalizeBulkConsoleTargetIDs(request.TargetIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	expectedConfirmation := bulkConsoleConfirmation(len(targetIDs))
	if request.Confirmation != expectedConfirmation {
		writeError(w, http.StatusBadRequest, "confirmation must be "+expectedConfirmation)
		return
	}

	targets := make([]console.Target, 0, len(targetIDs))
	for _, targetID := range targetIDs {
		target, _, err := s.serverSSHMaterialFromRuntime(r.Context(), runtime, targetID)
		if err != nil {
			handleServerSSHMaterialError(w, err)
			return
		}
		targets = append(targets, target)
	}

	items := make([]bulkConsoleCommandResponseItem, 0, len(targets))
	for _, target := range targets {
		requestID, err := s.insertCommandRequestWithOptions(r.Context(), runtime, commandRequestInsert{
			ServerID: target.ID,
			Source:   commandRequestSourceManual,
			Command:  request.Command,
			Reason:   request.Reason,
			Status:   "running",
		})
		if err != nil {
			writeInternalError(w)
			return
		}
		items = append(items, bulkConsoleCommandResponseItem{
			RequestID:  requestID,
			TargetID:   target.ID,
			TargetName: target.Name,
			Status:     "running",
		})
	}

	s.writeAudit(r.Context(), runtime, "user", nil, 0, "console.bulk_exec.started", map[string]any{
		"target_count": len(items),
		"request_ids":  bulkConsoleRequestIDs(items),
		"command":      request.Command,
	})
	s.runBulkConsoleCommands(runtime, request.Command, items)

	writeJSON(w, http.StatusAccepted, bulkConsoleCommandResponse{
		Parallelism: bulkConsoleCommandParallelism,
		Items:       items,
	})
}

func normalizeBulkConsoleTargetIDs(values []int64) ([]int64, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("target_ids is required")
	}
	if len(values) > bulkConsoleCommandMaxServers {
		return nil, fmt.Errorf("target_ids must contain %d targets or fewer", bulkConsoleCommandMaxServers)
	}
	seen := map[int64]bool{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value < 1 {
			return nil, fmt.Errorf("target_ids must contain positive ids")
		}
		if seen[value] {
			return nil, fmt.Errorf("target_ids must not contain duplicates")
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}

func bulkConsoleConfirmation(count int) string {
	return fmt.Sprintf("RUN ON %d SERVERS", count)
}

func bulkConsoleRequestIDs(items []bulkConsoleCommandResponseItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.RequestID)
	}
	return ids
}

func (s *Server) runBulkConsoleCommands(runtime *databaseRuntime, command string, items []bulkConsoleCommandResponseItem) {
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
				s.runBulkConsoleCommand(runtime, item.RequestID, item.TargetID, command)
			}()
		}
		wg.Wait()
	}()
}

func (s *Server) runBulkConsoleCommand(runtime *databaseRuntime, requestID int64, serverID int64, command string) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpInitialExecTimeout)
	defer cancel()

	result, err := runtime.consoleSessions.Exec(ctx, serverID, command)
	if err != nil {
		_ = s.finishCommandRequest(context.Background(), runtime, requestID, "error", 0, "", "", 0, sshCommandFailureMessage(err))
		return
	}
	if result.Running {
		_ = s.setCommandRequestSession(context.Background(), runtime, requestID, result.SessionID)
		s.finishActiveCommandRequest(runtime, requestID, serverID)
		return
	}
	status := "completed"
	if result.ExitCode != 0 {
		status = "failed"
	}
	_ = s.finishCommandRequest(context.Background(), runtime, requestID, status, result.SessionID, result.Output, "", result.ExitCode, "")
}
