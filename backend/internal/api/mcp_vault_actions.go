package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/projectcapabilities"
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
		project, err := resolveProjectRef(r.Context(), auth.runtime, projectRef)
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
	for projectIndex, project := range projects {
		remaining := maxMCPVaultItems - len(items)
		if remaining <= 0 {
			truncated = true
			break
		}
		visible, err := projectStore.TokenCanAccessProject(r.Context(), auth.TokenID, project.ID)
		if err != nil {
			writeInternalError(w)
			return
		}
		if !visible {
			continue
		}
		capability, err := projectcapabilities.NewStore(auth.runtime.database).Effective(
			r.Context(), auth.TokenID, project.ID, projectcapabilities.VaultMetadataRead, time.Now(),
		)
		if err != nil || capability.ExecutionRule != projectcapabilities.RuleAlwaysRun {
			continue
		}
		store, err := projectvault.NewStore(auth.runtime.database, auth.runtime.vault, auth.runtime.workspaceUUID)
		if err != nil {
			writeInternalError(w)
			return
		}
		projectItems, total, err := store.List(r.Context(), projectvault.ListFilter{ProjectID: project.ID, Limit: remaining})
		if err != nil {
			writeInternalError(w)
			return
		}
		if total > len(projectItems) || (len(items)+len(projectItems) >= maxMCPVaultItems && projectIndex < len(projects)-1) {
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
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	input.ProjectRef = strings.TrimSpace(input.ProjectRef)
	input.ActionName = strings.TrimSpace(input.ActionName)
	input.Reason = strings.TrimSpace(input.Reason)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.ProjectRef == "" || input.IdempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "project_ref and idempotency_key are required")
		return
	}
	if input.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	if len(input.IdempotencyKey) > 128 {
		writeError(w, http.StatusBadRequest, "idempotency_key is too long")
		return
	}
	if err := validateTextLimit("reason", input.Reason, maxReasonBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	project, err := resolveProjectRef(r.Context(), auth.runtime, input.ProjectRef)
	if errors.Is(err, projectstore.ErrNotFound) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	requestStore := vaultrequests.NewStore(auth.runtime.database)
	if _, existingErr := requestStore.GetByIdempotencyKey(r.Context(), auth.TokenID, input.IdempotencyKey); errors.Is(existingErr, vaultrequests.ErrNotFound) {
		if s.vaultRequestLimiter == nil ||
			!s.vaultRequestLimiter.allow("vault-request:"+auth.runtime.id+":"+strconv.FormatInt(auth.TokenID, 10)) {
			writeError(w, http.StatusTooManyRequests, "Vault action request rate limit exceeded; retry later")
			return
		}
	} else if existingErr != nil {
		writeInternalError(w)
		return
	}
	approval, contextHash, normalizedInput, err := buildVaultApprovalContext(
		r.Context(), s.Server, auth.runtime, auth.TokenID, project, input.ActionName, input.Input,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	initialStatus := vaultrequests.StatusApprovalPending
	if approval.ExecutionRule == projectcapabilities.RuleAlwaysRun {
		initialStatus = vaultrequests.StatusRunning
	}
	contextMap := map[string]any{}
	if payload, err := json.Marshal(approval); err == nil {
		_ = json.Unmarshal(payload, &contextMap)
	}
	runtimeID := (*int64)(nil)
	if approval.RuntimeID > 0 {
		runtimeID = &approval.RuntimeID
	}
	request, created, err := requestStore.Create(r.Context(), vaultrequests.CreateInput{
		TokenID: auth.TokenID, ProjectID: project.ID, RuntimeID: runtimeID,
		ActionName: input.ActionName, Input: normalizedInput, Reason: input.Reason,
		ApprovalContext: contextMap, ApprovalContextHash: contextHash,
		IdempotencyKey: input.IdempotencyKey,
		InitialStatus:  initialStatus,
	})
	if errors.Is(err, vaultrequests.ErrIdempotencyConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	if created && approval.ExecutionRule == projectcapabilities.RuleApprovalRequired {
		s.writeAudit(r.Context(), auth.runtime, "mcp", int64Ptr(auth.TokenID), approval.RuntimeID, "mcp.vault_action.approval_pending", map[string]any{
			"request_id": request.ID, "project_id": project.ID, "action_name": request.ActionName,
			"approval_context_hash": request.ApprovalContextHash,
		})
	}
	if created && approval.ExecutionRule == projectcapabilities.RuleAlwaysRun {
		executionContext, cancelExecution := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
		defer cancelExecution()
		result, runErr := s.executeClaimedVaultActionRequest(
			executionContext,
			auth.runtime,
			request,
			"mcp",
			"",
			"mcp.vault_action.always_run_requested",
			"mcp.vault_action",
		)
		if runErr != nil {
			writeInternalError(w)
			return
		}
		request = result.Request
	}
	response := vaultRequestMCPResponse(request)
	if request.Output != nil && !currentVaultPollAuthorization(r.Context(), s.Server, auth.runtime, request) {
		withholdVaultRequestOutput(response)
	}
	writeJSON(w, http.StatusOK, response)
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
	store := vaultrequests.NewStore(auth.runtime.database)
	item, err := store.Get(r.Context(), id)
	if errors.Is(err, vaultrequests.ErrNotFound) || (err == nil && item.TokenID != auth.TokenID) {
		writeError(w, http.StatusNotFound, "Vault action request not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	authorized := currentVaultPollAuthorization(r.Context(), s.Server, auth.runtime, item)
	if item.Status == vaultrequests.StatusApprovalPending && !authorized {
		stale, staleErr := store.StalePending(r.Context(), item.ID, "Vault approval context changed; send a fresh request")
		if staleErr == nil {
			item = stale
		}
	}
	response := vaultRequestMCPResponse(item)
	if item.Output != nil && !authorized {
		withholdVaultRequestOutput(response)
	}
	writeJSON(w, http.StatusOK, response)
}

func withholdVaultRequestOutput(response map[string]any) {
	delete(response, "output")
	response["output_withheld"] = true
	response["assistant_hint"] = "Current Vault authorization no longer permits returning this action output."
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
	item, err := vaultrequests.NewStore(auth.runtime.database).CancelOwned(r.Context(), id, auth.TokenID)
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
	s.writeAudit(r.Context(), auth.runtime, "mcp", int64Ptr(auth.TokenID), valueOrZero(item.RuntimeID), "mcp.vault_action.canceled", vaultActionAuditPayload(item, ""))
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

func currentVaultPollAuthorization(ctx context.Context, server *Server, runtime *databaseRuntime, item vaultrequests.Request) bool {
	var approval vaultApprovalContext
	if decodeMap(item.ApprovalContext, &approval) != nil ||
		approval.TokenID != item.TokenID || approval.ProjectID != item.ProjectID {
		return false
	}
	if _, err := validateVaultApprovalAuthorization(ctx, server, runtime, item, approval); err != nil {
		return false
	}
	if item.ActionName != vaultrequests.ActionRestartSession || item.Status != vaultrequests.StatusCompleted {
		return true
	}
	output, ok := item.Output.(map[string]any)
	if !ok {
		return false
	}
	sessionID := vaultJSONInt(output["session_id"])
	generation := vaultJSONInt(output["session_generation"])
	runtimeID := vaultJSONInt(output["runtime_id"])
	if sessionID < 1 || generation < 1 || runtimeID != approval.RuntimeID {
		return false
	}
	return vaultSessionObserveAuthorized(
		ctx,
		runtime,
		item.TokenID,
		sessionID,
		generation,
		runtimeID,
		true,
	)
}

func vaultJSONInt(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		return 0
	}
}
