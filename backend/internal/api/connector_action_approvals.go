package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type connectorActionApprovalHandlers struct{ *Server }

type declineConnectorActionApprovalRequest struct {
	UserNote string `json:"user_note"`
}

type runConnectorActionApprovalRequest struct {
	UserNote string `json:"user_note"`
}

type connectorActionApprovalItem struct {
	ID                  int64                  `json:"id"`
	TokenID             *int64                 `json:"token_id,omitempty"`
	TokenName           string                 `json:"token_name,omitempty"`
	TargetID            int64                  `json:"target_id"`
	TargetName          string                 `json:"target_name"`
	TargetRef           string                 `json:"target_ref"`
	ProfileID           int64                  `json:"profile_id"`
	ProfileLabel        string                 `json:"profile_label"`
	ConnectorKind       string                 `json:"connector_kind"`
	ActionName          string                 `json:"action_name"`
	Title               string                 `json:"title,omitempty"`
	Summary             string                 `json:"summary,omitempty"`
	Preview             map[string]any         `json:"preview,omitempty"`
	Input               map[string]any         `json:"input,omitempty"`
	Reason              string                 `json:"reason,omitempty"`
	Status              string                 `json:"status"`
	Output              any                    `json:"output,omitempty"`
	DisplayText         string                 `json:"display_text,omitempty"`
	Error               string                 `json:"error,omitempty"`
	RetryPolicy         connectors.RetryPolicy `json:"retry_policy"`
	ApprovalContextHash string                 `json:"approval_context_hash,omitempty"`
	CreatedAt           string                 `json:"created_at"`
	CompletedAt         *string                `json:"completed_at,omitempty"`
	RetryAfterSeconds   int                    `json:"retry_after_seconds,omitempty"`
	AssistantHint       string                 `json:"assistant_hint,omitempty"`
}

func (h connectorActionApprovalHandlers) listConnectorActionApprovals(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	query := r.URL.Query()
	filter, err := connectortargets.NewActionRequestFilter(query.Get("status"), query.Get("target_ref"), query.Get("action_name"), query.Get("active"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := connectortargets.NewStore(runtime.database).ListActionRequests(r.Context(), filter)
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

func (h connectorActionApprovalHandlers) getConnectorActionApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := h.activeRuntimeOrLocked(w)
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
	approval, err := h.connectorActionApprovalItemForResponse(r.Context(), runtime, item)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, approval)
}

func (h connectorActionApprovalHandlers) runConnectorActionApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	if !runtime.isMCPStarted() {
		writeError(w, http.StatusConflict, "MCP execution is stopped; start MCP from the web UI before running connector approvals")
		return
	}
	var request runConnectorActionApprovalRequest
	if r.ContentLength != 0 && !decodeJSON(w, r, &request) {
		return
	}
	request.UserNote = strings.TrimSpace(request.UserNote)
	if err := actions.ValidateApprovalNote(request.UserNote); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.runPendingConnectorAction(r.Context(), runtime, id, request.UserNote)
	if writeKnownApprovalError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, h.redactForPersistence(r.Context(), runtime, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, connectorActionApprovalItemFromRequest(item))
}

func (h connectorActionApprovalHandlers) declineConnectorActionApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request declineConnectorActionApprovalRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.UserNote = strings.TrimSpace(request.UserNote)
	if err := actions.ValidateApprovalNote(request.UserNote); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	workflow, err := h.connectorActionWorkflow(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	item, err := workflow.DeclinePending(r.Context(), id, request.UserNote)
	if writeKnownApprovalError(w, err) {
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, connectorActionApprovalItemFromRequest(item))
}

func writeKnownApprovalError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, connectortargets.ErrActionRequestNotFound) {
		writeError(w, http.StatusNotFound, "connector action request not found")
		return true
	}
	if errors.Is(err, connectortargets.ErrActionRequestNotPending) {
		writeError(w, http.StatusConflict, "connector action request is no longer pending")
		return true
	}
	if writeConnectorActionTerminalPersistenceError(w, err) {
		return true
	}
	return false
}

func (s *Server) runPendingConnectorAction(ctx context.Context, runtime *databaseRuntime, id int64, userNote string) (connectortargets.ActionRequest, error) {
	workflow, err := s.connectorActionWorkflow(runtime)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return workflow.RunPending(ctx, id, userNote)
}

func connectorActionApprovalItemFromRequest(item connectortargets.ActionRequest) connectorActionApprovalItem {
	response := connectorActionApprovalItem{
		ID: item.ID, TokenID: item.TokenID, TokenName: item.TokenName,
		TargetID: item.TargetID, TargetName: item.TargetName,
		TargetRef: connectors.FormatTargetRef(item.ConnectorKind, item.TargetID, item.ProfileID),
		ProfileID: item.ProfileID, ProfileLabel: item.ProfileLabel,
		ConnectorKind: item.ConnectorKind, ActionName: item.ActionName,
		Title: item.Title, Summary: item.Summary, Preview: item.Preview, Input: item.Input,
		Reason: item.Reason, Status: string(item.Status), Output: item.Output,
		DisplayText: item.DisplayText, Error: item.Error, RetryPolicy: item.RetryPolicy,
		ApprovalContextHash: item.ApprovalContextHash, CreatedAt: item.CreatedAt, CompletedAt: item.CompletedAt,
	}
	if item.Status == connectors.ResultApprovalPending {
		response.RetryAfterSeconds = 3
		response.AssistantHint = connectorActionApprovalHint
	}
	return response
}

func (s *Server) connectorActionApprovalItemForResponse(ctx context.Context, runtime *databaseRuntime, item connectortargets.ActionRequest) (connectorActionApprovalItem, error) {
	response := connectorActionApprovalItemFromRequest(item)
	workflow, err := s.connectorActionWorkflow(runtime)
	if err != nil {
		return connectorActionApprovalItem{}, err
	}
	preview, err := workflow.ApprovalPreview(ctx, item)
	if err != nil {
		return connectorActionApprovalItem{}, err
	}
	response.Preview = preview
	return response, nil
}
