package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type mcpConnectorActionCallRequest struct {
	TargetRef      string         `json:"target_ref"`
	ActionName     string         `json:"action_name"`
	Input          map[string]any `json:"input,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
}

type mcpConnectorActionResponse = actions.Response

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
	response := connectorActionToMCPResponse(s.connectorAdapterRegistry(), result.Request, result.Result)
	response.Replayed = result.Replayed
	s.writeMCPConnectorActionResponse(w, r, auth.runtime, auth.TokenID, result.Request, response)
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
	response := connectorActionToMCPResponse(s.connectorAdapterRegistry(), request, connectors.ActionResult{
		Status: request.Status, Output: request.Output, DisplayText: request.DisplayText, Error: request.Error,
	})
	s.writeMCPConnectorActionResponse(w, r, auth.runtime, auth.TokenID, request, response)
}

func connectorActionResponseForToken(ctx context.Context, adapterRegistry *connectorapi.Registry, runtime *databaseRuntime, tokenID int64, request connectortargets.ActionRequest, result connectors.ActionResult) mcpConnectorActionResponse {
	response := connectorActionToMCPResponse(adapterRegistry, request, result)
	if !connectorActionVaultPollAuthorized(ctx, runtime, tokenID, request) {
		actions.Withhold(&response)
	}
	return response
}

func (s mcpHandlers) writeMCPConnectorActionResponse(
	w http.ResponseWriter,
	r *http.Request,
	runtime *databaseRuntime,
	tokenID int64,
	request connectortargets.ActionRequest,
	response mcpConnectorActionResponse,
) {
	encoded, err := json.Marshal(response)
	if err != nil {
		writeInternalError(w)
		return
	}
	release, err := runtime.vaultDelivery.acquireDelivery(r.Context())
	if err != nil {
		return
	}
	defer release()
	if !connectorActionVaultPollAuthorizedLocked(r.Context(), runtime, tokenID, request) {
		actions.Withhold(&response)
		encoded, err = json.Marshal(response)
		if err != nil {
			writeInternalError(w)
			return
		}
	}
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(encoded, '\n'))
}

func connectorActionVaultPollAuthorized(ctx context.Context, runtime *databaseRuntime, tokenID int64, request connectortargets.ActionRequest) bool {
	if runtime == nil || runtime.tokens == nil || runtime.database == nil || request.TokenID == nil || *request.TokenID != tokenID {
		return false
	}
	release, err := runtime.vaultDelivery.acquireDelivery(ctx)
	if err != nil {
		return false
	}
	defer release()
	return connectorActionVaultPollAuthorizedLocked(ctx, runtime, tokenID, request)
}

// connectorActionVaultPollAuthorizedLocked requires runtime.vaultDelivery.
func connectorActionVaultPollAuthorizedLocked(ctx context.Context, runtime *databaseRuntime, tokenID int64, request connectortargets.ActionRequest) bool {
	if !runtime.isMCPStarted() {
		return false
	}
	token, err := runtime.tokens.Get(ctx, tokenID)
	if err != nil || !tokens.Active(token.RevokedAt, token.ExpiresAt, time.Now().UTC()) {
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
	principal, err := tokenExecutionPrincipal(runtime, tokenID)
	if err != nil {
		return false
	}
	return vaultsessions.NewObserver(runtime.database, runtime.vaultLeases).Authorized(
		ctx,
		principal,
		vaultsessions.ObserveRequest{
			SessionID: *request.SessionID, SessionGeneration: *request.SessionGeneration,
		},
	)
}

func connectorActionToMCPResponse(adapterRegistry *connectorapi.Registry, request connectortargets.ActionRequest, result connectors.ActionResult) mcpConnectorActionResponse {
	return actions.FromResult(request, result, connectorActionResponseRunningHint(adapterRegistry, request))
}

func connectorActionRequestToMCPResponse(adapterRegistry *connectorapi.Registry, request connectortargets.ActionRequest) mcpConnectorActionResponse {
	return actions.FromRequest(request, connectorActionResponseRunningHint(adapterRegistry, request))
}

func connectorActionResponseRunningHint(adapterRegistry *connectorapi.Registry, request connectortargets.ActionRequest) string {
	if request.Status != connectors.ResultRunning {
		return ""
	}
	return connectorActionRunningHintForRequest(adapterRegistry, request)
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
