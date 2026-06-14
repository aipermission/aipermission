package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

const (
	bulkConsoleCommandMaxTargets  = 25
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

	targets := make([]bulkConsoleTarget, 0, len(targetIDs))
	for _, targetID := range targetIDs {
		target, err := s.bulkConsoleTarget(r.Context(), runtime, targetID)
		if err != nil {
			handleConnectorTargetRuntimeError(w, err)
			return
		}
		targets = append(targets, target)
	}

	items := make([]bulkConsoleCommandResponseItem, 0, len(targets))
	for _, target := range targets {
		requestID, err := s.insertCommandRequestWithOptions(r.Context(), runtime, commandRequestInsert{
			RuntimeProfileID: target.ID,
			Source:           commandRequestSourceManual,
			Command:          request.Command,
			Reason:           request.Reason,
			Status:           "running",
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

type bulkConsoleTarget struct {
	ID   int64
	Name string
}

func (s consoleHandlers) bulkConsoleTarget(ctx context.Context, runtime *databaseRuntime, runtimeID int64) (bulkConsoleTarget, error) {
	targetRef, err := liveConsoleTargetRefForRuntimeID(ctx, runtime, runtimeID)
	if err != nil {
		return bulkConsoleTarget{}, err
	}
	target, profile, err := connectortargets.NewStore(runtime.database).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return bulkConsoleTarget{}, err
	}
	name := target.Name
	if adapter := connectorLiveConsoleTargetAdapterFor(target.ConnectorKind); adapter != nil {
		metadata := adapter.LiveConsoleTargetMetadata(connectors.TargetView{
			ID:            target.ID,
			ConnectorKind: target.ConnectorKind,
			Name:          target.Name,
			Config:        target.Config,
		}, connectors.CredentialProfileView{
			ID:            profile.ID,
			TargetID:      profile.TargetID,
			ConnectorKind: profile.ConnectorKind,
			Kind:          profile.Kind,
			Label:         profile.Label,
			Public:        profile.Public,
		})
		if label, _ := metadata["label"].(string); strings.TrimSpace(label) != "" {
			name = strings.TrimSpace(label)
		}
	}
	return bulkConsoleTarget{ID: runtimeID, Name: name}, nil
}

func handleConnectorTargetRuntimeError(w http.ResponseWriter, err error) {
	if errors.Is(err, connectortargets.ErrTargetProfileNotFound) ||
		errors.Is(err, connectortargets.ErrTargetNotFound) ||
		errors.Is(err, connectortargets.ErrInvalidTargetRef) {
		writeError(w, http.StatusNotFound, "connector target profile not found")
		return
	}
	writeInternalError(w)
}

func normalizeBulkConsoleTargetIDs(values []int64) ([]int64, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("target_ids is required")
	}
	if len(values) > bulkConsoleCommandMaxTargets {
		return nil, fmt.Errorf("target_ids must contain %d targets or fewer", bulkConsoleCommandMaxTargets)
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
	return fmt.Sprintf("RUN ON %d TARGETS", count)
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

func (s *Server) runBulkConsoleCommand(runtime *databaseRuntime, requestID int64, runtimeProfileID int64, command string) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpInitialExecTimeout)
	defer cancel()

	result, err := runtime.consoleSessions.Exec(ctx, runtimeProfileID, command)
	if err != nil {
		adapter := s.bulkConsoleErrorPresenter(context.Background(), runtime, runtimeProfileID)
		_ = s.finishCommandRequest(context.Background(), runtime, requestID, "error", 0, "", "", 0, connectorErrorMessage(adapter, "command execution failed", err))
		return
	}
	if result.Running {
		_ = s.setCommandRequestSession(context.Background(), runtime, requestID, result.SessionID)
		s.finishActiveCommandRequest(runtime, requestID, runtimeProfileID)
		return
	}
	status := "completed"
	if result.ExitCode != 0 {
		status = "failed"
	}
	_ = s.finishCommandRequest(context.Background(), runtime, requestID, status, result.SessionID, result.Output, "", result.ExitCode, "")
}

func (s *Server) bulkConsoleErrorPresenter(ctx context.Context, runtime *databaseRuntime, runtimeID int64) any {
	targetRef, err := liveConsoleTargetRefForRuntimeID(ctx, runtime, runtimeID)
	if err != nil {
		return nil
	}
	target, _, err := connectortargets.NewStore(runtime.database).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return nil
	}
	return connectorAPIAdapterFor(target.ConnectorKind)
}
