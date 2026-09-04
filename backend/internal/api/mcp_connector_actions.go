package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type mcpConnectorTargetItem struct {
	TargetRef     string                    `json:"target_ref"`
	ProjectID     int64                     `json:"project_id"`
	ProjectName   string                    `json:"project_name"`
	ProjectSlug   string                    `json:"project_slug"`
	TargetID      int64                     `json:"target_id"`
	TargetName    string                    `json:"target_name"`
	ConnectorKind string                    `json:"connector_kind"`
	ProfileID     int64                     `json:"profile_id"`
	ProfileLabel  string                    `json:"profile_label"`
	ProfileKind   string                    `json:"profile_kind"`
	Metadata      map[string]any            `json:"metadata,omitempty"`
	Actions       []mcpConnectorActionGrant `json:"actions"`
	Hints         []string                  `json:"hints,omitempty"`
}

type mcpConnectorActionGrant struct {
	Name          string `json:"name"`
	ExecutionRule string `json:"execution_rule"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

type mcpConnectorActionCallRequest struct {
	TargetRef      string         `json:"target_ref"`
	ActionName     string         `json:"action_name"`
	Input          map[string]any `json:"input,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
}

type mcpConnectorActionResponse struct {
	Status            string                 `json:"status"`
	RequestID         int64                  `json:"request_id,omitempty"`
	TargetRef         string                 `json:"target_ref"`
	TargetName        string                 `json:"target_name,omitempty"`
	ConnectorKind     string                 `json:"connector_kind"`
	ProfileLabel      string                 `json:"profile_label,omitempty"`
	ActionName        string                 `json:"action_name"`
	Input             map[string]any         `json:"input,omitempty"`
	Output            any                    `json:"output,omitempty"`
	DisplayText       string                 `json:"display_text,omitempty"`
	Error             string                 `json:"error,omitempty"`
	RetryPolicy       connectors.RetryPolicy `json:"retry_policy"`
	RetryAfterSeconds int                    `json:"retry_after_seconds,omitempty"`
	AssistantHint     string                 `json:"assistant_hint,omitempty"`
	OutputWithheld    bool                   `json:"output_withheld,omitempty"`
	Replayed          bool                   `json:"replayed,omitempty"`
}

func (s mcpHandlers) mcpListConnectorTargets(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	permissions, err := projectScopedSupportedConnectorPermissions(r.Context(), auth.runtime, auth.TokenID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	settings, err := readSecuritySettings(r.Context(), auth.runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	store := connectortargets.NewStore(auth.runtime.database)
	itemsByRef := map[string]*mcpConnectorTargetItem{}
	order := []string{}
	for _, permission := range permissions {
		if permission.ExecutionRule == connectortargets.ActionPermissionBlocked {
			continue
		}
		ref := connectortargets.ConnectorTargetRef(permission.ConnectorKind, permission.TargetID, permission.ProfileID)
		item := itemsByRef[ref]
		if item == nil {
			item = &mcpConnectorTargetItem{
				TargetRef:     ref,
				ProjectID:     permission.ProjectID,
				ProjectName:   permission.ProjectName,
				ProjectSlug:   permission.ProjectSlug,
				TargetID:      permission.TargetID,
				TargetName:    permission.TargetName,
				ConnectorKind: permission.ConnectorKind,
				ProfileID:     permission.ProfileID,
				ProfileLabel:  permission.ProfileLabel,
				ProfileKind:   permission.ProfileKind,
				Hints:         connectorTargetHints(permission.ConnectorKind),
			}
			if settings.ExposeMCPServerMetadata {
				target, profile, err := connectorTargetProfileViews(r.Context(), store, permission.TargetID, permission.ProfileID)
				if err != nil {
					handleConnectorTargetError(w, err)
					return
				}
				item.Metadata = s.connectorMCPMetadata(target, profile)
			}
			itemsByRef[ref] = item
			order = append(order, ref)
		}
		item.Actions = append(item.Actions, mcpConnectorActionGrant{
			Name:          permission.ActionName,
			ExecutionRule: string(permission.ExecutionRule),
			ExpiresAt:     permission.ExpiresAt,
		})
	}
	items := make([]mcpConnectorTargetItem, 0, len(order))
	for _, ref := range order {
		items = append(items, *itemsByRef[ref])
	}
	writeJSON(w, http.StatusOK, items)
}

func (s mcpHandlers) connectorMCPMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
	if adapter := s.connectorLiveConsoleTargetAdapterFor(target.ConnectorKind); adapter != nil {
		return adapter.LiveConsoleTargetMetadata(target, profile)
	}
	return nil
}

func (s mcpHandlers) mcpGetConnectorHelp(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	target, _, connector, ok := s.resolveMCPConnectorTarget(w, r, auth)
	if !ok {
		return
	}
	help, err := connector.GetHelp(r.Context(), target)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, help)
}

func (s mcpHandlers) mcpGetConnectorActions(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	target, profile, connector, ok := s.resolveMCPConnectorTarget(w, r, auth)
	if !ok {
		return
	}
	actions, err := connectors.GetActionDefinitions(r.Context(), connector, target, profile)
	if err != nil {
		writeInternalError(w)
		return
	}
	allowed, err := permittedConnectorActions(r.Context(), auth.runtime, auth.TokenID, target.ID, profile.ID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	filtered := make([]connectors.ActionDefinition, 0, len(actions))
	for _, action := range actions {
		if allowed[action.Name] {
			filtered = append(filtered, action)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": filtered})
}

func (s mcpHandlers) mcpCallConnectorAction(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	var request mcpConnectorActionCallRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.TargetRef = strings.TrimSpace(request.TargetRef)
	request.ActionName = strings.TrimSpace(request.ActionName)
	request.Reason = strings.TrimSpace(request.Reason)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.TargetRef == "" {
		writeError(w, http.StatusBadRequest, "target_ref is required")
		return
	}
	if !connectors.ValidIdentifier(request.ActionName) {
		writeError(w, http.StatusBadRequest, "invalid action_name")
		return
	}
	if err := validateTextLimit("reason", request.Reason, maxReasonBytes); err != nil {
		writeErrorWithCode(w, http.StatusBadRequest, s.redactForPersistence(r.Context(), auth.runtime, err.Error()), connectors.ErrorCode(err))
		return
	}
	if len(request.IdempotencyKey) > connectortargets.MaxIdempotencyKeyBytes {
		writeError(w, http.StatusBadRequest, "idempotency_key is too long")
		return
	}
	result, err := s.callConnectorAction(r.Context(), auth.runtime, connectorActionCall{
		Source:         commandRequestSourceMCP,
		TokenID:        auth.TokenID,
		TargetRef:      request.TargetRef,
		ActionName:     request.ActionName,
		Input:          request.Input,
		Reason:         request.Reason,
		IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		if writeConnectorActionTerminalPersistenceError(w, err) {
			return
		}
		if errors.Is(err, errMCPExecutionStopped) {
			writeStoppedMCP(w)
			return
		}
		if errors.Is(err, connectortargets.ErrActionRequestIdempotency) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, connectortargets.ErrInvalidTargetRef) || errors.Is(err, connectortargets.ErrTargetProfileNotFound) {
			handleConnectorTargetError(w, err)
			return
		}
		writeErrorWithCode(w, http.StatusBadRequest, s.redactForPersistence(r.Context(), auth.runtime, err.Error()), connectors.ErrorCode(err))
		return
	}
	auditAction := "mcp.connector_action." + string(result.Result.Status)
	if result.Replayed {
		auditAction = "mcp.connector_action.replayed"
	}
	s.writeObservationAudit(r.Context(), auth.runtime, "mcp", int64Ptr(auth.TokenID), 0, auditAction, map[string]any{
		"request_id":     result.Request.ID,
		"target_ref":     request.TargetRef,
		"connector_kind": result.Request.ConnectorKind,
		"action_name":    request.ActionName,
		"replayed":       result.Replayed,
	})
	response := connectorActionResponseForToken(r.Context(), s.connectorAdapterRegistry(), auth.runtime, auth.TokenID, result.Request, result.Result)
	response.Replayed = result.Replayed
	writeJSON(w, http.StatusOK, response)
}

func (s mcpHandlers) mcpGetConnectorActionRequest(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	request, err := connectortargets.NewStore(auth.runtime.database).GetActionRequest(r.Context(), id)
	if errors.Is(err, connectortargets.ErrActionRequestNotFound) {
		writeError(w, http.StatusNotFound, "connector action request not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	if request.TokenID == nil || *request.TokenID != auth.TokenID {
		writeError(w, http.StatusNotFound, "connector action request not found")
		return
	}
	response := connectorActionResponseForToken(r.Context(), s.connectorAdapterRegistry(), auth.runtime, auth.TokenID, request, connectors.ActionResult{
		Status: request.Status, Output: request.Output, DisplayText: request.DisplayText, Error: request.Error,
	})
	writeJSON(w, http.StatusOK, response)
}

func connectorActionResponseForToken(ctx context.Context, adapterRegistry *connectorapi.Registry, runtime *databaseRuntime, tokenID int64, request connectortargets.ActionRequest, result connectors.ActionResult) mcpConnectorActionResponse {
	response := connectorActionToMCPResponse(adapterRegistry, request, result)
	if !connectorActionVaultPollAuthorized(ctx, runtime, tokenID, request) {
		response.Output = nil
		response.DisplayText = ""
		response.OutputWithheld = true
		const authorizationHint = "Current Vault session authorization no longer permits returning this connector output."
		if response.AssistantHint == "" {
			response.AssistantHint = authorizationHint
		} else {
			response.AssistantHint += " " + authorizationHint
		}
	}
	return response
}

func connectorActionVaultPollAuthorized(ctx context.Context, runtime *databaseRuntime, tokenID int64, request connectortargets.ActionRequest) bool {
	if runtime == nil || runtime.tokens == nil || runtime.database == nil || request.TokenID == nil || *request.TokenID != tokenID {
		return false
	}
	release, err := runtime.vaultDelivery.acquire(ctx)
	if err != nil {
		return false
	}
	defer release()
	token, err := runtime.tokens.Get(ctx, tokenID)
	if err != nil || token.RevokedAt != "" || expired(token.ExpiresAt, time.Now().UTC()) {
		return false
	}
	permission, err := connectortargets.NewStore(runtime.database).GetActionPermission(
		ctx,
		tokenID,
		request.TargetID,
		request.ProfileID,
		request.ActionName,
		time.Now().UTC(),
	)
	if err != nil || permission.ExecutionRule == connectortargets.ActionPermissionBlocked {
		return false
	}
	if request.SessionID == nil && request.SessionGeneration == nil {
		return true
	}
	if request.SessionID == nil || request.SessionGeneration == nil {
		return false
	}
	return vaultSessionObserveAuthorized(
		ctx,
		runtime,
		tokenID,
		*request.SessionID,
		*request.SessionGeneration,
		0,
		false,
	)
}

func (s mcpHandlers) resolveMCPConnectorTarget(w http.ResponseWriter, r *http.Request, auth mcpAuthContext) (connectors.TargetView, connectors.CredentialProfileView, connectors.Connector, bool) {
	targetRef := strings.TrimSpace(r.URL.Query().Get("target_ref"))
	if targetRef == "" {
		writeError(w, http.StatusBadRequest, "target_ref is required")
		return connectors.TargetView{}, connectors.CredentialProfileView{}, nil, false
	}
	target, profile, err := connectortargets.NewStore(auth.runtime.database).ResolveConnectorActionTarget(r.Context(), targetRef)
	if err != nil {
		handleConnectorTargetError(w, err)
		return connectors.TargetView{}, connectors.CredentialProfileView{}, nil, false
	}
	permissions, err := projectScopedSupportedConnectorPermissions(r.Context(), auth.runtime, auth.TokenID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return connectors.TargetView{}, connectors.CredentialProfileView{}, nil, false
	}
	allowed := false
	for _, permission := range permissions {
		if permission.TargetID == target.ID && permission.ProfileID == profile.ID && permission.ExecutionRule != connectortargets.ActionPermissionBlocked {
			allowed = true
			break
		}
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "token has no active connector actions for this target/profile")
		return connectors.TargetView{}, connectors.CredentialProfileView{}, nil, false
	}
	connector, ok := auth.runtime.connectorRegistry().Get(target.ConnectorKind)
	if !ok {
		writeError(w, http.StatusNotFound, "connector not found")
		return connectors.TargetView{}, connectors.CredentialProfileView{}, nil, false
	}
	return target, profile, connector, true
}

func permittedConnectorActions(ctx context.Context, runtime *databaseRuntime, tokenID int64, targetID int64, profileID int64) (map[string]bool, error) {
	permissions, err := projectScopedSupportedConnectorPermissions(ctx, runtime, tokenID)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, permission := range permissions {
		if permission.TargetID != targetID || permission.ProfileID != profileID {
			continue
		}
		if permission.ExecutionRule == connectortargets.ActionPermissionBlocked {
			continue
		}
		allowed[permission.ActionName] = true
	}
	return allowed, nil
}

func connectorActionToMCPResponse(adapterRegistry *connectorapi.Registry, request connectortargets.ActionRequest, result connectors.ActionResult) mcpConnectorActionResponse {
	response := connectorActionRequestToMCPResponse(adapterRegistry, request)
	response.Output = result.Output
	if result.DisplayText != "" {
		response.DisplayText = result.DisplayText
	}
	if result.Error != "" {
		response.Error = result.Error
	}
	return response
}

func connectorActionRequestToMCPResponse(adapterRegistry *connectorapi.Registry, request connectortargets.ActionRequest) mcpConnectorActionResponse {
	response := mcpConnectorActionResponse{
		Status:        string(request.Status),
		RequestID:     request.ID,
		TargetRef:     connectortargets.ConnectorTargetRef(request.ConnectorKind, request.TargetID, request.ProfileID),
		TargetName:    request.TargetName,
		ConnectorKind: request.ConnectorKind,
		ProfileLabel:  request.ProfileLabel,
		ActionName:    request.ActionName,
		Input:         request.Input,
		Output:        request.Output,
		DisplayText:   request.DisplayText,
		Error:         request.Error,
		RetryPolicy:   request.RetryPolicy,
	}
	if request.Status == connectors.ResultApprovalPending {
		response.RetryAfterSeconds = 3
		response.AssistantHint = connectorActionApprovalHint
	}
	if request.Status == connectors.ResultRunning {
		response.RetryAfterSeconds = 3
		response.AssistantHint = connectorActionRunningHintForRequest(adapterRegistry, request)
	}
	if request.Status == connectors.ResultOutcomeUnknown {
		response.AssistantHint = "Do not retry this action automatically. The operation may have been dispatched; inspect external state with a connector-specific read-only action first."
	}
	return response
}

func connectorActionRunningHintForRequest(adapterRegistry *connectorapi.Registry, request connectortargets.ActionRequest) string {
	adapter, _ := adapterRegistry.For(request.ConnectorKind).(connectorapi.RuntimeAdapter)
	if adapter != nil {
		if hint := strings.TrimSpace(adapter.RunningHint(request)); hint != "" {
			return hint
		}
	}
	return "Wait 3 seconds, then call get_connector_action_request again until this request is completed, failed, canceled, stale, or error."
}

func connectorTargetHints(_ string) []string {
	return []string{
		"Use get_connector_help and get_connector_actions before calling connector actions for the first time.",
		"Target, credential profile, and token action permission decide what the connector can do; prefer approval_required until the workflow is trusted.",
	}
}
