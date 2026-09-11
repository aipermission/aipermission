package vaultrequests

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

const MaxMCPVaultItems = 100

type MCPActionCallRequest struct {
	ProjectRef     string         `json:"project_ref"`
	ActionName     string         `json:"action_name"`
	Input          map[string]any `json:"input"`
	Reason         string         `json:"reason"`
	IdempotencyKey string         `json:"idempotency_key"`
}

type MCPVaultItem struct {
	VaultRef         string `json:"vault_ref"`
	ItemID           int64  `json:"item_id"`
	ProjectRef       string `json:"project_ref"`
	SourceProjectID  int64  `json:"source_project_id"`
	Name             string `json:"name"`
	SecretType       string `json:"secret_type"`
	Status           string `json:"status"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	ValueVersion     int64  `json:"value_version"`
	MetadataRevision int64  `json:"metadata_revision"`
}

type MCPHTTPScope struct {
	Database      *sql.DB
	Vault         *vault.Vault
	WorkspaceUUID string
	TokenID       int64
	MCPStarted    func() bool
	Runtime       func(context.Context) (Application, error)
	MetadataRead  func(context.Context, int64) (bool, error)
}

type MCPHTTPScopeProvider func(http.ResponseWriter, *http.Request) (MCPHTTPScope, bool)

type MCPHTTPHandlers struct{ scope MCPHTTPScopeProvider }

func NewMCPHTTPHandlers(scope MCPHTTPScopeProvider) *MCPHTTPHandlers {
	return &MCPHTTPHandlers{scope: scope}
}

func (h *MCPHTTPHandlers) ListItems(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, r, true, false)
	if !ok {
		return
	}
	projectRef := strings.TrimSpace(r.URL.Query().Get("project_ref"))
	projects := projectstore.NewStore(scope.Database)
	var visibleProjects []projectstore.Project
	if projectRef != "" {
		project, err := projects.ResolveRef(r.Context(), projectRef)
		if errors.Is(err, projectstore.ErrNotFound) {
			httptransport.WriteError(w, http.StatusNotFound, "project not found")
			return
		}
		if err != nil {
			httptransport.WriteInternalError(w)
			return
		}
		visibleProjects = []projectstore.Project{project}
	} else {
		var err error
		visibleProjects, err = projects.List(r.Context())
		if err != nil {
			httptransport.WriteInternalError(w)
			return
		}
	}
	store, err := projectvault.NewStore(scope.Database, scope.Vault, scope.WorkspaceUUID)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	items := make([]MCPVaultItem, 0)
	truncated := false
	for _, project := range visibleProjects {
		visible, err := projects.TokenCanAccessProject(r.Context(), scope.TokenID, project.ID)
		if err != nil {
			httptransport.WriteInternalError(w)
			return
		}
		if !visible {
			continue
		}
		allowed, err := scope.MetadataRead(r.Context(), project.ID)
		if err != nil || !allowed {
			continue
		}
		remaining := MaxMCPVaultItems - len(items)
		queryLimit := max(remaining, 1)
		projectItems, total, err := store.List(r.Context(), projectvault.ListFilter{ProjectID: project.ID, Limit: queryLimit})
		if err != nil {
			httptransport.WriteInternalError(w)
			return
		}
		if remaining < 1 {
			if total > 0 {
				truncated = true
				break
			}
			continue
		}
		if total > len(projectItems) {
			truncated = true
		}
		for _, item := range projectItems {
			items = append(items, MCPVaultItem{
				VaultRef: "vault:" + strconv.FormatInt(item.ID, 10), ItemID: item.ID,
				ProjectRef: project.Slug, SourceProjectID: project.ID, Name: item.Name,
				SecretType: item.SecretType, Status: item.Status, ExpiresAt: item.ExpiresAt,
				ValueVersion: item.ValueVersion, MetadataRevision: item.MetadataRevision,
			})
		}
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items, "count": len(items), "truncated": truncated, "secret_values_returned": false,
	})
}

func (h *MCPHTTPHandlers) Call(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, r, false, true)
	if !ok {
		return
	}
	if !scope.MCPStarted() {
		writeStoppedMCP(w)
		return
	}
	var input MCPActionCallRequest
	if !httptransport.DecodeJSON(w, r, &input, 0) {
		return
	}
	owner, ok := h.runtime(w, r, scope)
	if !ok {
		return
	}
	view, err := owner.Call(r.Context(), CallInput{
		TokenID: scope.TokenID, ProjectRef: input.ProjectRef, ActionName: input.ActionName,
		Input: input.Input, Reason: input.Reason, IdempotencyKey: input.IdempotencyKey,
	})
	if writeMCPCallError(w, err) {
		return
	}
	writeMCPResponse(w, view)
}

func (h *MCPHTTPHandlers) GetRequest(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, r, false, false)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	owner, ok := h.runtime(w, r, scope)
	if !ok {
		return
	}
	view, err := owner.GetOwned(r.Context(), id, scope.TokenID)
	if errors.Is(err, ErrNotFound) {
		httptransport.WriteError(w, http.StatusNotFound, "Vault action request not found")
		return
	}
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	writeMCPResponse(w, view)
}

func (h *MCPHTTPHandlers) CancelRequest(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, r, false, false)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	owner, ok := h.runtime(w, r, scope)
	if !ok {
		return
	}
	item, err := owner.CancelOwned(r.Context(), id, scope.TokenID)
	switch {
	case errors.Is(err, ErrNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "Vault action request not found")
	case errors.Is(err, ErrNotPending):
		httptransport.WriteError(w, http.StatusConflict, "Vault action request is no longer pending")
	case err != nil:
		httptransport.WriteInternalError(w)
	default:
		httptransport.WriteJSON(w, http.StatusOK, MCPResponse(item))
	}
}

func (h *MCPHTTPHandlers) resolve(w http.ResponseWriter, r *http.Request, requireCatalog, requireMCPState bool) (MCPHTTPScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return MCPHTTPScope{}, false
	}
	scope, ok := h.scope(w, r)
	if !ok {
		return MCPHTTPScope{}, false
	}
	invalid := scope.TokenID < 1 || scope.Runtime == nil
	invalid = invalid || requireMCPState && scope.MCPStarted == nil
	invalid = invalid || requireCatalog && (scope.Database == nil || scope.Vault == nil ||
		strings.TrimSpace(scope.WorkspaceUUID) == "" || scope.MetadataRead == nil)
	if invalid {
		httptransport.WriteInternalError(w)
		return MCPHTTPScope{}, false
	}
	return scope, true
}

func (h *MCPHTTPHandlers) runtime(w http.ResponseWriter, r *http.Request, scope MCPHTTPScope) (Application, bool) {
	owner, err := scope.Runtime(r.Context())
	if err != nil || owner == nil || owner.Validate() != nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	return owner, true
}

func writeMCPResponse(w http.ResponseWriter, view RequestView) {
	response := MCPResponse(view.Request)
	if view.Request.Output != nil && !view.OutputAuthorized {
		delete(response, "output")
		response["output_withheld"] = true
		response["assistant_hint"] = "Current Vault authorization no longer permits returning this action output."
	}
	httptransport.WriteJSON(w, http.StatusOK, response)
}

func writeMCPCallError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var validation ValidationError
	var preparation PreparationError
	switch {
	case errors.As(err, &validation), errors.As(err, &preparation):
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrProjectNotFound):
		httptransport.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrIdempotencyConflict):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrRequestRateLimited):
		httptransport.WriteError(w, http.StatusTooManyRequests, err.Error())
	default:
		httptransport.WriteInternalError(w)
	}
	return true
}

func MCPResponse(item Request) map[string]any {
	response := map[string]any{
		"request_id": item.ID, "status": item.Status, "project_ref": item.ProjectSlug,
		"action_name": item.ActionName, "input": item.Input, "reason": item.Reason,
		"created_at": item.CreatedAt, "expires_at": item.ExpiresAt, "secret_values_returned": false,
	}
	if item.Output != nil {
		response["output"] = item.Output
	}
	if item.Error != "" {
		response["error"] = item.Error
	}
	if item.Status == StatusApprovalPending {
		response["retry_after_seconds"] = 3
		response["assistant_hint"] = "Wait for the local user to approve or decline, then poll get_vault_action_request."
	}
	return response
}

func writeStoppedMCP(w http.ResponseWriter) {
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "stopped",
		"error":  "MCP execution is stopped in the local gateway. Start MCP from the web UI before running commands.",
	})
}
