package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type connectorActionApprovalHandlers struct {
	*Server
}

type declineConnectorActionApprovalRequest struct {
	UserNote string `json:"user_note"`
}

type runConnectorActionApprovalRequest struct {
	UserNote string `json:"user_note"`
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
	Title               string         `json:"title,omitempty"`
	Summary             string         `json:"summary,omitempty"`
	Preview             map[string]any `json:"preview,omitempty"`
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
	approval, err := connectorActionApprovalItemForResponse(runtime, item)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, approval)
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
	var request runConnectorActionApprovalRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
	}
	request.UserNote = strings.TrimSpace(request.UserNote)
	if err := validateTextLimit("user_note", request.UserNote, maxMessageBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := s.runPendingConnectorAction(r.Context(), runtime, id, request.UserNote)
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
	var request declineConnectorActionApprovalRequest
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
	var item connectortargets.ActionRequest
	err := s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "connector_action.request.declined",
		func() any { return connectorActionRequestAuditPayload(item) },
		func(tx *sql.Tx) error {
			var err error
			item, err = connectortargets.NewTxStore(tx).DeclineActionRequest(r.Context(), id, message)
			return err
		},
	)
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
	s.writeObservationAudit(r.Context(), runtime, "user", item.TokenID, 0, "connector_action.decline", map[string]any{
		"request_id":     item.ID,
		"target_ref":     connectortargets.ConnectorTargetRef(item.ConnectorKind, item.TargetID, item.ProfileID),
		"connector_kind": item.ConnectorKind,
		"action_name":    item.ActionName,
		"note":           request.UserNote != "",
	})
	writeJSON(w, http.StatusOK, connectorActionApprovalItemFromRequest(item))
}

func (s *Server) runPendingConnectorAction(ctx context.Context, runtime *databaseRuntime, id int64, userNote string) (connectortargets.ActionRequest, error) {
	release, err := runtime.vaultDelivery.acquire(ctx)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	claimHeld := true
	defer func() {
		if claimHeld {
			release()
		}
	}()
	store := connectortargets.NewStore(runtime.database)
	item, err := store.GetActionRequest(ctx, id)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if item.Status != connectors.ResultApprovalPending {
		return connectortargets.ActionRequest{}, connectortargets.ErrActionRequestNotPending
	}
	if item.TokenID == nil {
		stale, staleErr := s.finishStaleConnectorApproval(ctx, runtime, item.ID, "connector approval token no longer exists", "token")
		if staleErr != nil {
			return connectortargets.ActionRequest{}, staleErr
		}
		return stale, fmt.Errorf("connector approval token no longer exists; ask the AI to send a fresh request")
	}
	tokenID := *item.TokenID
	token, err := runtime.tokens.Get(ctx, tokenID)
	if err != nil {
		stale, staleErr := s.finishStaleConnectorApproval(ctx, runtime, item.ID, "connector approval token no longer exists; ask the AI to send a fresh request", "token")
		if staleErr != nil {
			return connectortargets.ActionRequest{}, staleErr
		}
		return stale, fmt.Errorf("connector approval token no longer exists; ask the AI to send a fresh request")
	}
	if token.RevokedAt != "" || expired(token.ExpiresAt, time.Now().UTC()) {
		reason := "connector approval token is no longer valid; ask the AI to send a fresh request"
		if token.RevokedAt != "" {
			reason = "connector approval token was revoked; ask the AI to send a fresh request"
		} else {
			reason = "connector approval token expired; ask the AI to send a fresh request"
		}
		stale, staleErr := s.finishStaleConnectorApproval(ctx, runtime, item.ID, reason, "token")
		if staleErr != nil {
			return connectortargets.ActionRequest{}, staleErr
		}
		return stale, fmt.Errorf("%s", reason)
	}
	rawInput, rawPayload, rawReason, err := connectorActionExecutionPayload(runtime, item)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	targetRef := connectortargets.ConnectorTargetRef(item.ConnectorKind, item.TargetID, item.ProfileID)
	prepared, err := runtime.prepareConnectorAction(ctx, actions.PrepareRequest{
		Source:     commandRequestSourceMCP,
		TargetRef:  targetRef,
		ActionName: item.ActionName,
		Input:      rawInput,
		Reason:     rawReason,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		reason := "connector approval context changed; ask the AI to send a fresh request"
		stale, staleErr := s.finishStaleConnectorApproval(ctx, runtime, item.ID, reason, "target_or_action")
		if staleErr != nil {
			return connectortargets.ActionRequest{}, staleErr
		}
		return stale, fmt.Errorf("%s", reason)
	}
	permission, err := store.GetActionPermission(ctx, tokenID, item.TargetID, item.ProfileID, item.ActionName, time.Now().UTC())
	if err != nil || permission.ExecutionRule != connectortargets.ActionPermissionApprovalRequired {
		stale, staleErr := s.finishStaleConnectorApproval(ctx, runtime, item.ID, "connector approval context changed; ask the AI to send a fresh request", "permission")
		if staleErr != nil {
			return connectortargets.ActionRequest{}, staleErr
		}
		if err != nil && !errors.Is(err, connectortargets.ErrActionPermissionNotFound) {
			return stale, err
		}
		return stale, fmt.Errorf("connector approval context changed; ask the AI to send a fresh request")
	}
	currentContext, currentHash, err := connectorApprovalContext(prepared, token, permission, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if item.ApprovalContextHash != "" && item.ApprovalContextHash != currentHash {
		drift := connectorApprovalDriftReason(item.ApprovalContext, currentContext)
		stale, staleErr := s.finishStaleConnectorApproval(ctx, runtime, item.ID, "connector approval context changed; ask the AI to send a fresh request", drift)
		if staleErr != nil {
			return connectortargets.ActionRequest{}, staleErr
		}
		return stale, fmt.Errorf("connector approval context changed; ask the AI to send a fresh request")
	}
	if rawPayload != nil {
		prepared.Action.Payload = rawPayload
	}
	if userNote != "" {
		if _, err := s.insertMessage(ctx, runtime, createMessageRequest{
			TokenID:   tokenID,
			Direction: "user_to_ai",
			Message:   "Operator approved the connector action with note: " + userNote,
		}); err != nil {
			return connectortargets.ActionRequest{}, err
		}
	}
	principal, err := tokenExecutionPrincipal(runtime, tokenID)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	snapshot, err := s.snapshotPreparedConnectorAction(ctx, runtime, prepared)
	if err != nil {
		reason := "connector approval context changed; ask the AI to send a fresh request"
		stale, staleErr := s.finishStaleConnectorApproval(ctx, runtime, item.ID, reason, "profile")
		if staleErr != nil {
			return connectortargets.ActionRequest{}, staleErr
		}
		return stale, fmt.Errorf("%s", reason)
	}
	var running connectortargets.ActionRequest
	if err := s.withAuditedMutation(
		ctx, runtime, "user", item.TokenID, 0, "connector_action.request.running",
		func() any { return connectorActionRequestAuditPayload(running) },
		func(tx *sql.Tx) error {
			var err error
			running, err = connectortargets.NewTxStore(tx).MarkActionRequestRunning(ctx, item.ID)
			return err
		},
	); err != nil {
		return connectortargets.ActionRequest{}, err
	}
	release()
	claimHeld = false
	result, err := s.executePreparedConnectorAction(ctx, runtime, principal, prepared, snapshot)
	if err != nil {
		failureOutput := connectorActionFailureOutput(err)
		finished, finishErr := s.finishConnectorActionRequest(context.Background(), runtime, item.ID, connectors.ResultFailed, failureOutput, "", err.Error(), prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectortargets.ActionRequest{}, finishErr
		}
		return finished, nil
	}
	status := result.Status
	if status == "" {
		status = connectors.ResultCompleted
	}
	item, err = s.captureConnectorActionSessionHandleIfReturned(ctx, runtime, item, result.Handles)
	if err != nil {
		finished, finishErr := s.finishConnectorActionRequest(context.Background(), runtime, item.ID, connectors.ResultOutcomeUnknown, nil, "", connectorActionHandleError, prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectortargets.ActionRequest{}, errors.Join(err, finishErr)
		}
		return finished, nil
	}
	if status == connectors.ResultRunning {
		if !connectorActionSupportsRunning(prepared) {
			finished, finishErr := s.finishConnectorActionRequest(context.Background(), runtime, item.ID, connectors.ResultError, nil, "", "connector returned running for an action that does not support asynchronous execution", prepared.ActionDefinition.OutputHint)
			if finishErr != nil {
				return connectortargets.ActionRequest{}, finishErr
			}
			s.writeObservationAudit(ctx, runtime, "user", item.TokenID, 0, "connector_action.run.error", map[string]any{
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
		go s.finishActiveConnectorActionRequest(runtime, item.ID, prepared, principal, result.Handles)
		running, err := store.GetActionRequest(context.Background(), item.ID)
		if err != nil {
			return connectortargets.ActionRequest{}, err
		}
		s.writeObservationAudit(ctx, runtime, "user", item.TokenID, 0, "connector_action.run.running", map[string]any{
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
	result = s.redactConnectorActionResult(context.Background(), runtime, result, prepared.ActionDefinition.OutputHint)
	finished, err := s.finishConnectorActionRequest(
		context.Background(), runtime, item.ID, status,
		result.Output, result.DisplayText, result.Error, prepared.ActionDefinition.OutputHint,
	)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	s.writeObservationAudit(ctx, runtime, "user", item.TokenID, 0, "connector_action.run."+string(finished.Status), map[string]any{
		"request_id":     item.ID,
		"target_ref":     targetRef,
		"connector_kind": item.ConnectorKind,
		"action_name":    item.ActionName,
		"note":           userNote != "",
	})
	return finished, nil
}

func (s *Server) finishStaleConnectorApproval(ctx context.Context, runtime *databaseRuntime, requestID int64, reason string, drift string) (connectortargets.ActionRequest, error) {
	var stale connectortargets.ActionRequest
	err := s.withAuditedMutation(
		ctx, runtime, "gateway", nil, 0, "connector_action.request.stale",
		func() any { return connectorActionRequestAuditPayload(stale) },
		func(tx *sql.Tx) error {
			var err error
			stale, err = connectortargets.NewTxStore(tx).FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
				ID: requestID, Status: connectors.ResultStale, Error: reason,
				ApprovalDrift: drift, AllowedStatuses: connectorApprovalFinishStatuses(),
			})
			return err
		},
	)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return stale, nil
}

func connectorApprovalFinishStatuses() []connectors.ResultStatus {
	return []connectors.ResultStatus{connectors.ResultApprovalPending, connectors.ResultRunning}
}

func connectorActionExecutionPayload(runtime *databaseRuntime, item connectortargets.ActionRequest) (map[string]any, map[string]any, string, error) {
	if item.EncryptedPayloadJSON == "" {
		return cloneMapAny(item.Input), nil, item.Reason, nil
	}
	var envelope connectorActionExecutionEnvelope
	if err := runtime.vault.DecryptJSON(item.EncryptedPayloadJSON, &envelope); err == nil && (envelope.Input != nil || envelope.Payload != nil) {
		reason := envelope.Reason
		if reason == "" {
			reason = item.Reason
		}
		return cloneMapAny(envelope.Input), cloneMapAny(envelope.Payload), reason, nil
	}
	var payload map[string]any
	if err := runtime.vault.DecryptJSON(item.EncryptedPayloadJSON, &payload); err != nil {
		return nil, nil, "", err
	}
	return cloneMapAny(payload), cloneMapAny(payload), item.Reason, nil
}

func cloneMapAny(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
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
		Title:               item.Title,
		Summary:             item.Summary,
		Preview:             item.Preview,
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

func connectorActionApprovalItemForResponse(runtime *databaseRuntime, item connectortargets.ActionRequest) (connectorActionApprovalItem, error) {
	response := connectorActionApprovalItemFromRequest(item)
	if item.Status != connectors.ResultApprovalPending || item.EncryptedPayloadJSON == "" {
		return response, nil
	}
	var envelope connectorActionExecutionEnvelope
	if err := runtime.vault.DecryptJSON(item.EncryptedPayloadJSON, &envelope); err != nil {
		return connectorActionApprovalItem{}, fmt.Errorf("decrypt connector approval preview: %w", err)
	}
	if envelope.ApprovalPreview != nil {
		response.Preview = cloneMapAny(envelope.ApprovalPreview)
	}
	return response, nil
}
