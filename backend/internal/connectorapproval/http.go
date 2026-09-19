// Package connectorapproval owns the user-facing connector action approval
// transport and its projection of persisted action requests.
package connectorapproval

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const noAutomaticRetryHint = "Do not retry automatically. Inspect the recorded request and external target state first."

type Scope struct {
	Requests   RequestStore
	Workflow   func() (Workflow, error)
	MCPStarted func() bool
	Redact     func(context.Context, string) string
}

type RequestStore interface {
	ListActionRequests(context.Context, connectortargets.ActionRequestFilter) ([]connectortargets.ActionRequest, error)
	GetActionRequest(context.Context, int64) (connectortargets.ActionRequest, error)
}

type Workflow interface {
	ApprovalPreview(context.Context, connectortargets.ActionRequest) (map[string]any, error)
	RunPending(context.Context, int64, string) (connectortargets.ActionRequest, error)
	DeclinePending(context.Context, int64, string) (connectortargets.ActionRequest, error)
}

type ScopeProvider func(http.ResponseWriter) (Scope, bool)

type HTTPHandlers struct{ scope ScopeProvider }

type NoteRequest struct {
	UserNote            string `json:"user_note"`
	ApprovalContextHash string `json:"approval_context_hash"`
}

type Item struct {
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

type scopeRequirement uint8

const (
	requireRequests scopeRequirement = 1 << iota
	requireWorkflow
	requireMCPState
	requireRedactor
)

func NewHTTPHandlers(scope ScopeProvider) *HTTPHandlers { return &HTTPHandlers{scope: scope} }

func (h *HTTPHandlers) List(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireRequests)
	if !ok {
		return
	}
	query := r.URL.Query()
	filter, err := connectortargets.NewActionRequestFilter(query.Get("status"), query.Get("target_ref"), query.Get("action_name"), query.Get("active"))
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := scope.Requests.ListActionRequests(r.Context(), filter)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	response := make([]Item, 0, len(items))
	for _, item := range items {
		response = append(response, ItemFromRequest(item))
	}
	httptransport.WriteJSON(w, http.StatusOK, response)
}

func (h *HTTPHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	scope, ok := h.resolve(w, requireRequests|requireWorkflow)
	if !ok {
		return
	}
	item, err := scope.Requests.GetActionRequest(r.Context(), id)
	if errors.Is(err, connectortargets.ErrActionRequestNotFound) {
		httptransport.WriteError(w, http.StatusNotFound, "connector action request not found")
		return
	}
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	workflow, err := scope.Workflow()
	if err != nil || workflow == nil {
		httptransport.WriteInternalError(w)
		return
	}
	approval, err := ItemForResponse(r.Context(), workflow, item)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, approval)
}

func (h *HTTPHandlers) Run(w http.ResponseWriter, r *http.Request) {
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	scope, ok := h.resolve(w, requireWorkflow|requireMCPState|requireRedactor)
	if !ok {
		return
	}
	if !scope.MCPStarted() {
		httptransport.WriteError(w, http.StatusConflict, "MCP execution is stopped; start MCP from the web UI before running connector approvals")
		return
	}
	if scope.Requests == nil {
		httptransport.WriteInternalError(w)
		return
	}
	request := NoteRequest{}
	if r.ContentLength != 0 && !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	request.UserNote = strings.TrimSpace(request.UserNote)
	if err := actions.ValidateApprovalNote(request.UserNote); err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.validateDecisionContext(w, r, scope.Requests, id, request.ApprovalContextHash) {
		return
	}
	workflow, err := scope.Workflow()
	if err != nil || workflow == nil {
		httptransport.WriteInternalError(w)
		return
	}
	item, err := workflow.RunPending(r.Context(), id, request.UserNote)
	if writeKnownError(w, err) {
		return
	}
	if err != nil {
		httptransport.WriteError(w, http.StatusConflict, scope.Redact(r.Context(), err.Error()))
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, ItemFromRequest(item))
}

func (h *HTTPHandlers) Decline(w http.ResponseWriter, r *http.Request) {
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	scope, ok := h.resolve(w, requireRequests|requireWorkflow)
	if !ok {
		return
	}
	request := NoteRequest{}
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	request.UserNote = strings.TrimSpace(request.UserNote)
	if err := actions.ValidateApprovalNote(request.UserNote); err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.validateDecisionContext(w, r, scope.Requests, id, request.ApprovalContextHash) {
		return
	}
	workflow, err := scope.Workflow()
	if err != nil || workflow == nil {
		httptransport.WriteInternalError(w)
		return
	}
	item, err := workflow.DeclinePending(r.Context(), id, request.UserNote)
	if writeKnownError(w, err) {
		return
	}
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, ItemFromRequest(item))
}

func (h *HTTPHandlers) validateDecisionContext(w http.ResponseWriter, r *http.Request, store RequestStore, id int64, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		httptransport.WriteError(w, http.StatusBadRequest, "approval_context_hash is required")
		return false
	}
	item, err := store.GetActionRequest(r.Context(), id)
	if errors.Is(err, connectortargets.ErrActionRequestNotFound) {
		httptransport.WriteError(w, http.StatusNotFound, "connector action request not found")
		return false
	}
	if err != nil {
		httptransport.WriteInternalError(w)
		return false
	}
	if item.ApprovalContextHash == "" || item.ApprovalContextHash != expected {
		httptransport.WriteError(w, http.StatusConflict, "approval context changed; refresh and review the request again")
		return false
	}
	return true
}

func ItemFromRequest(item connectortargets.ActionRequest) Item {
	response := Item{
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
		response.AssistantHint = actions.ApprovalHint
	}
	return response
}

func ItemForResponse(ctx context.Context, workflow Workflow, item connectortargets.ActionRequest) (Item, error) {
	if workflow == nil {
		return Item{}, actions.ErrWorkflowUnavailable
	}
	response := ItemFromRequest(item)
	preview, err := workflow.ApprovalPreview(ctx, item)
	if err != nil {
		return Item{}, err
	}
	response.Preview = preview
	return response, nil
}

func (h *HTTPHandlers) resolve(w http.ResponseWriter, requirements scopeRequirement) (Scope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return Scope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return Scope{}, false
	}
	valid := requirements&requireRequests == 0 || scope.Requests != nil
	valid = valid && (requirements&requireWorkflow == 0 || scope.Workflow != nil)
	valid = valid && (requirements&requireMCPState == 0 || scope.MCPStarted != nil)
	valid = valid && (requirements&requireRedactor == 0 || scope.Redact != nil)
	if !valid {
		httptransport.WriteInternalError(w)
		return Scope{}, false
	}
	return scope, true
}

func writeKnownError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, connectortargets.ErrActionRequestNotFound) {
		httptransport.WriteError(w, http.StatusNotFound, "connector action request not found")
		return true
	}
	if errors.Is(err, connectortargets.ErrActionRequestNotPending) {
		httptransport.WriteError(w, http.StatusConflict, "connector action request is no longer pending")
		return true
	}
	var persistenceErr *actions.TerminalPersistenceError
	if !errors.As(err, &persistenceErr) {
		return false
	}
	httptransport.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
		"status":         connectors.ResultOutcomeUnknown,
		"code":           "connector_action_persistence_unknown",
		"request_id":     persistenceErr.RequestID,
		"error":          actions.TerminalPersistenceErrorText,
		"assistant_hint": noAutomaticRetryHint,
	})
	return true
}
