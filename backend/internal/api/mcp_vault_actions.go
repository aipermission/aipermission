package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type mcpVaultActionCallRequest struct {
	ProjectRef     string         `json:"project_ref"`
	ActionName     string         `json:"action_name"`
	Input          map[string]any `json:"input"`
	Reason         string         `json:"reason"`
	IdempotencyKey string         `json:"idempotency_key"`
}

type mcpVaultItem struct {
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

const maxMCPVaultItems = 100

func (s mcpHandlers) mcpListVaultItems(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	projectRef := strings.TrimSpace(r.URL.Query().Get("project_ref"))
	projectStore := projectstore.NewStore(auth.runtime.database)
	var projects []projectstore.Project
	if projectRef != "" {
		project, err := projectStore.ResolveRef(r.Context(), projectRef)
		if errors.Is(err, projectstore.ErrNotFound) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		if err != nil {
			writeInternalError(w)
			return
		}
		projects = []projectstore.Project{project}
	} else {
		var err error
		projects, err = projectStore.List(r.Context())
		if err != nil {
			writeInternalError(w)
			return
		}
	}
	items := []mcpVaultItem{}
	truncated := false
	for _, project := range projects {
		visible, err := projectStore.TokenCanAccessProject(r.Context(), auth.TokenID, project.ID)
		if err != nil {
			writeInternalError(w)
			return
		}
		if !visible {
			continue
		}
		capability, err := accesscontrol.NewCapabilityStore(auth.runtime.database).Effective(
			r.Context(), auth.TokenID, project.ID, accesscontrol.VaultMetadataRead, time.Now(),
		)
		if err != nil || capability.ExecutionRule != accesscontrol.RuleAlwaysRun {
			continue
		}
		store, err := projectvault.NewStore(auth.runtime.database, auth.runtime.vault, auth.runtime.workspaceUUID)
		if err != nil {
			writeInternalError(w)
			return
		}
		remaining := maxMCPVaultItems - len(items)
		queryLimit := remaining
		if queryLimit < 1 {
			queryLimit = 1
		}
		projectItems, total, err := store.List(r.Context(), projectvault.ListFilter{ProjectID: project.ID, Limit: queryLimit})
		if err != nil {
			writeInternalError(w)
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
			items = append(items, mcpVaultItem{
				VaultRef: "vault:" + strconv.FormatInt(item.ID, 10), ItemID: item.ID,
				ProjectRef: project.Slug, SourceProjectID: project.ID,
				Name: item.Name, SecretType: item.SecretType, Status: item.Status,
				ExpiresAt: item.ExpiresAt, ValueVersion: item.ValueVersion,
				MetadataRevision: item.MetadataRevision,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "count": len(items), "truncated": truncated,
		"secret_values_returned": false,
	})
}

func (s mcpHandlers) mcpCallVaultAction(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	if s.rejectStoppedMCP(w, auth.runtime) {
		return
	}
	var input mcpVaultActionCallRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	owner, err := s.vaultRequestRuntime(r.Context(), auth.runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	view, err := owner.Call(r.Context(), vaultrequests.CallInput{
		TokenID: auth.TokenID, ProjectRef: input.ProjectRef, ActionName: input.ActionName,
		Input: input.Input, Reason: input.Reason, IdempotencyKey: input.IdempotencyKey,
	})
	if writeVaultCallError(w, err) {
		return
	}
	writeVaultRequestMCPResponse(w, view)
}

func (s mcpHandlers) mcpGetVaultActionRequest(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	owner, err := s.vaultRequestRuntime(r.Context(), auth.runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	view, err := owner.GetOwned(r.Context(), id, auth.TokenID)
	if errors.Is(err, vaultrequests.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Vault action request not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	writeVaultRequestMCPResponse(w, view)
}

func withholdVaultRequestOutput(response map[string]any) {
	delete(response, "output")
	response["output_withheld"] = true
	response["assistant_hint"] = "Current Vault authorization no longer permits returning this action output."
}

func writeVaultRequestMCPResponse(w http.ResponseWriter, view vaultrequests.RequestView) {
	response := vaultRequestMCPResponse(view.Request)
	if view.Request.Output != nil && !view.OutputAuthorized {
		withholdVaultRequestOutput(response)
	}
	writeJSON(w, http.StatusOK, response)
}

func writeVaultCallError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var validation vaultrequests.ValidationError
	var preparation vaultrequests.PreparationError
	switch {
	case errors.As(err, &validation), errors.As(err, &preparation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, vaultrequests.ErrProjectNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, vaultrequests.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, vaultrequests.ErrRequestRateLimited):
		writeError(w, http.StatusTooManyRequests, err.Error())
	default:
		writeInternalError(w)
	}
	return true
}

func (s mcpHandlers) mcpCancelVaultActionRequest(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	owner, err := s.vaultRequestRuntime(r.Context(), auth.runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	item, err := owner.CancelOwned(r.Context(), id, auth.TokenID)
	if errors.Is(err, vaultrequests.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Vault action request not found")
		return
	}
	if errors.Is(err, vaultrequests.ErrNotPending) {
		writeError(w, http.StatusConflict, "Vault action request is no longer pending")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, vaultRequestMCPResponse(item))
}

func vaultRequestMCPResponse(item vaultrequests.Request) map[string]any {
	response := map[string]any{
		"request_id": item.ID, "status": item.Status, "project_ref": item.ProjectSlug,
		"action_name": item.ActionName, "input": item.Input, "reason": item.Reason,
		"created_at": item.CreatedAt, "expires_at": item.ExpiresAt,
		"secret_values_returned": false,
	}
	if item.Output != nil {
		response["output"] = item.Output
	}
	if item.Error != "" {
		response["error"] = item.Error
	}
	if item.Status == vaultrequests.StatusApprovalPending {
		response["retry_after_seconds"] = 3
		response["assistant_hint"] = "Wait for the local user to approve or decline, then poll get_vault_action_request."
	}
	return response
}
