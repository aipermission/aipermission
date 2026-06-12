package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/history"
)

type connectorActionApprovalHandlers struct {
	*Server
}

type connectorActionApprovalItem struct {
	ID                  int64          `json:"id"`
	TokenID             *int64         `json:"token_id,omitempty"`
	TokenName           string         `json:"token_name,omitempty"`
	TargetID            int64          `json:"target_id"`
	TargetName          string         `json:"target_name"`
	TargetRef           string         `json:"target_ref"`
	ProfileID           int64          `json:"profile_id"`
	ProfileLabel        string         `json:"profile_label"`
	ConnectorKind       string         `json:"connector_kind"`
	ActionName          string         `json:"action_name"`
	Input               map[string]any `json:"input,omitempty"`
	Reason              string         `json:"reason,omitempty"`
	Status              string         `json:"status"`
	Output              any            `json:"output,omitempty"`
	DisplayText         string         `json:"display_text,omitempty"`
	Error               string         `json:"error,omitempty"`
	ApprovalContextHash string         `json:"approval_context_hash,omitempty"`
	CreatedAt           string         `json:"created_at"`
	CompletedAt         *string        `json:"completed_at,omitempty"`
	RetryAfterSeconds   int            `json:"retry_after_seconds,omitempty"`
	AssistantHint       string         `json:"assistant_hint,omitempty"`
}

func (s connectorActionApprovalHandlers) listConnectorActionApprovals(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	items, err := connectortargets.NewStore(runtime.database).ListActionRequests(r.Context(), connectortargets.ActionRequestFilter{
		Status: status,
		Limit:  100,
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	response := make([]connectorActionApprovalItem, 0, len(items))
	for _, item := range items {
		response = append(response, connectorActionApprovalItemFromRequest(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s connectorActionApprovalHandlers) getConnectorActionApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	item, err := connectortargets.NewStore(runtime.database).GetActionRequest(r.Context(), id)
	if errors.Is(err, connectortargets.ErrActionRequestNotFound) {
		writeError(w, http.StatusNotFound, "connector action request not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, connectorActionApprovalItemFromRequest(item))
}

func (s connectorActionApprovalHandlers) runConnectorActionApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	if !runtime.isMCPStarted() {
		writeError(w, http.StatusConflict, "MCP execution is stopped; start MCP from the web UI before running connector approvals")
		return
	}
	item, err := s.runPendingConnectorAction(r.Context(), runtime, id)
	if errors.Is(err, connectortargets.ErrActionRequestNotFound) {
		writeError(w, http.StatusNotFound, "connector action request not found")
		return
	}
	if errors.Is(err, connectortargets.ErrActionRequestNotPending) {
		writeError(w, http.StatusConflict, "connector action request is no longer pending")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, s.redactForPersistence(r.Context(), runtime, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, connectorActionApprovalItemFromRequest(item))
}

func (s connectorActionApprovalHandlers) declineConnectorActionApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request declineApprovalRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.UserNote = strings.TrimSpace(request.UserNote)
	if err := validateTextLimit("user_note", request.UserNote, maxMessageBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	message := "User declined the connector action"
	if request.UserNote != "" {
		message = message + ": " + request.UserNote
	}
	item, err := connectortargets.NewStore(runtime.database).DeclineActionRequest(r.Context(), id, message)
	if errors.Is(err, connectortargets.ErrActionRequestNotFound) {
		writeError(w, http.StatusNotFound, "connector action request not found")
		return
	}
	if errors.Is(err, connectortargets.ErrActionRequestNotPending) {
		writeError(w, http.StatusConflict, "connector action request is no longer pending")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := history.NewStore(runtime.database).SyncConnectorActionRequest(r.Context(), item.ID); err != nil {
		writeInternalError(w)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", item.TokenID, 0, "connector_action.decline", map[string]any{
		"request_id":     item.ID,
		"target_ref":     connectortargets.ConnectorTargetRef(item.ConnectorKind, item.TargetID, item.ProfileID),
		"connector_kind": item.ConnectorKind,
		"action_name":    item.ActionName,
		"note":           request.UserNote != "",
	})
	writeJSON(w, http.StatusOK, connectorActionApprovalItemFromRequest(item))
}

func (s *Server) runPendingConnectorAction(ctx context.Context, runtime *databaseRuntime, id int64) (connectortargets.ActionRequest, error) {
	store := connectortargets.NewStore(runtime.database)
	item, err := store.GetActionRequest(ctx, id)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if item.Status != connectors.ResultApprovalPending {
		return connectortargets.ActionRequest{}, connectortargets.ErrActionRequestNotPending
	}
	if item.TokenID == nil {
		stale, staleErr := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
			ID:     item.ID,
			Status: connectors.ResultStale,
			Error:  "connector approval token no longer exists",
		})
		if staleErr != nil {
			return connectortargets.ActionRequest{}, staleErr
		}
		_ = history.NewStore(runtime.database).SyncConnectorActionRequest(context.Background(), stale.ID)
		return stale, fmt.Errorf("connector approval token no longer exists; ask the AI to send a fresh request")
	}
	tokenID := *item.TokenID
	token, err := runtime.tokens.Get(ctx, tokenID)
	if err != nil {
		stale, staleErr := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
			ID:     item.ID,
			Status: connectors.ResultStale,
			Error:  "connector approval token no longer exists; ask the AI to send a fresh request",
		})
		if staleErr != nil {
			return connectortargets.ActionRequest{}, staleErr
		}
		_ = history.NewStore(runtime.database).SyncConnectorActionRequest(context.Background(), stale.ID)
		return stale, fmt.Errorf("connector approval token no longer exists; ask the AI to send a fresh request")
	}
	if token.RevokedAt != "" || expired(token.ExpiresAt, time.Now().UTC()) {
		reason := "connector approval token is no longer valid; ask the AI to send a fresh request"
		if token.RevokedAt != "" {
			reason = "connector approval token was revoked; ask the AI to send a fresh request"
		} else {
			reason = "connector approval token expired; ask the AI to send a fresh request"
		}
		stale, staleErr := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
			ID:     item.ID,
			Status: connectors.ResultStale,
			Error:  reason,
		})
		if staleErr != nil {
			return connectortargets.ActionRequest{}, staleErr
		}
		_ = history.NewStore(runtime.database).SyncConnectorActionRequest(context.Background(), stale.ID)
		return stale, fmt.Errorf("%s", reason)
	}
	targetRef := connectortargets.ConnectorTargetRef(item.ConnectorKind, item.TargetID, item.ProfileID)
	prepared, err := runtime.prepareConnectorAction(ctx, actions.PrepareRequest{
		Source:     commandRequestSourceMCP,
		TargetRef:  targetRef,
		ActionName: item.ActionName,
		Input:      item.Input,
		Reason:     item.Reason,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	permission, err := store.GetActionPermission(ctx, tokenID, item.TargetID, item.ProfileID, item.ActionName, time.Now().UTC())
	if err != nil || permission.ExecutionRule != connectortargets.ActionPermissionApprovalRequired {
		stale, staleErr := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
			ID:     item.ID,
			Status: connectors.ResultStale,
			Error:  "connector approval context changed; ask the AI to send a fresh request",
		})
		if staleErr != nil {
			return connectortargets.ActionRequest{}, staleErr
		}
		_ = history.NewStore(runtime.database).SyncConnectorActionRequest(context.Background(), stale.ID)
		if err != nil && !errors.Is(err, connectortargets.ErrActionPermissionNotFound) {
			return stale, err
		}
		return stale, fmt.Errorf("connector approval context changed; ask the AI to send a fresh request")
	}
	_, currentHash, err := connectorApprovalContext(prepared, token, permission, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if item.ApprovalContextHash != "" && item.ApprovalContextHash != currentHash {
		stale, staleErr := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
			ID:     item.ID,
			Status: connectors.ResultStale,
			Error:  "connector approval context changed; ask the AI to send a fresh request",
		})
		if staleErr != nil {
			return connectortargets.ActionRequest{}, staleErr
		}
		_ = history.NewStore(runtime.database).SyncConnectorActionRequest(context.Background(), stale.ID)
		return stale, fmt.Errorf("connector approval context changed; ask the AI to send a fresh request")
	}
	payload := map[string]any{}
	if item.EncryptedPayloadJSON != "" {
		if err := runtime.vault.DecryptJSON(item.EncryptedPayloadJSON, &payload); err != nil {
			return connectortargets.ActionRequest{}, err
		}
		prepared.Action.Payload = payload
	}
	if _, err := store.MarkActionRequestRunning(ctx, item.ID); err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if err := history.NewStore(runtime.database).SyncConnectorActionRequest(ctx, item.ID); err != nil {
		return connectortargets.ActionRequest{}, err
	}
	result, err := s.executePreparedConnectorAction(ctx, runtime, prepared)
	if err != nil {
		finished, finishErr := s.finishConnectorActionRequest(context.Background(), runtime, item.ID, connectors.ResultFailed, nil, "", err.Error())
		if finishErr != nil {
			return connectortargets.ActionRequest{}, finishErr
		}
		return finished, nil
	}
	status := result.Status
	if status == "" {
		status = connectors.ResultCompleted
	}
	if status == connectors.ResultRunning {
		if !connectorActionSupportsRunning(prepared) {
			finished, finishErr := s.finishConnectorActionRequest(context.Background(), runtime, item.ID, connectors.ResultError, nil, "", "connector returned running for an action that does not support asynchronous execution")
			if finishErr != nil {
				return connectortargets.ActionRequest{}, finishErr
			}
			s.writeAudit(ctx, runtime, "user", item.TokenID, 0, "connector_action.run.error", map[string]any{
				"request_id":     item.ID,
				"target_ref":     targetRef,
				"connector_kind": item.ConnectorKind,
				"action_name":    item.ActionName,
			})
			return finished, nil
		}
		result.Handles.RequestID = item.ID
		if result.Handles.FollowupTool == "" {
			result.Handles.FollowupTool = "get_connector_action_request"
		}
		go s.finishActiveConnectorActionRequest(runtime, item.ID, prepared)
		running, err := store.GetActionRequest(context.Background(), item.ID)
		if err != nil {
			return connectortargets.ActionRequest{}, err
		}
		s.writeAudit(ctx, runtime, "user", item.TokenID, 0, "connector_action.run.running", map[string]any{
			"request_id":     item.ID,
			"target_ref":     targetRef,
			"connector_kind": item.ConnectorKind,
			"action_name":    item.ActionName,
		})
		return running, nil
	}
	if status == connectors.ResultApprovalPending {
		status = connectors.ResultFailed
		result.Error = "connector returned approval_pending after approval was already granted"
	}
	result = s.redactConnectorActionResult(context.Background(), runtime, result)
	finished, err := store.FinishActionRequest(context.Background(), connectortargets.FinishActionRequestInput{
		ID:          item.ID,
		Status:      status,
		Output:      result.Output,
		DisplayText: result.DisplayText,
		Error:       result.Error,
	})
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if err := history.NewStore(runtime.database).SyncConnectorActionRequest(context.Background(), finished.ID); err != nil {
		return connectortargets.ActionRequest{}, err
	}
	s.writeAudit(ctx, runtime, "user", item.TokenID, 0, "connector_action.run."+string(finished.Status), map[string]any{
		"request_id":     item.ID,
		"target_ref":     targetRef,
		"connector_kind": item.ConnectorKind,
		"action_name":    item.ActionName,
	})
	return finished, nil
}

func connectorActionApprovalItemFromRequest(item connectortargets.ActionRequest) connectorActionApprovalItem {
	response := connectorActionApprovalItem{
		ID:                  item.ID,
		TokenID:             item.TokenID,
		TokenName:           item.TokenName,
		TargetID:            item.TargetID,
		TargetName:          item.TargetName,
		TargetRef:           connectortargets.ConnectorTargetRef(item.ConnectorKind, item.TargetID, item.ProfileID),
		ProfileID:           item.ProfileID,
		ProfileLabel:        item.ProfileLabel,
		ConnectorKind:       item.ConnectorKind,
		ActionName:          item.ActionName,
		Input:               item.Input,
		Reason:              item.Reason,
		Status:              string(item.Status),
		Output:              item.Output,
		DisplayText:         item.DisplayText,
		Error:               item.Error,
		ApprovalContextHash: item.ApprovalContextHash,
		CreatedAt:           item.CreatedAt,
		CompletedAt:         item.CompletedAt,
	}
	if item.Status == connectors.ResultApprovalPending {
		response.RetryAfterSeconds = 3
		response.AssistantHint = connectorActionApprovalHint
	}
	return response
}
