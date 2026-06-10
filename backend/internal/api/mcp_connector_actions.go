package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type mcpConnectorTargetItem struct {
	TargetRef     string                    `json:"target_ref"`
	TargetID      int64                     `json:"target_id"`
	TargetName    string                    `json:"target_name"`
	ConnectorKind string                    `json:"connector_kind"`
	ProfileID     int64                     `json:"profile_id"`
	ProfileLabel  string                    `json:"profile_label"`
	ProfileKind   string                    `json:"profile_kind"`
	Actions       []mcpConnectorActionGrant `json:"actions"`
	Hints         []string                  `json:"hints,omitempty"`
}

type mcpConnectorActionGrant struct {
	Name          string `json:"name"`
	ExecutionRule string `json:"execution_rule"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

type mcpConnectorActionCallRequest struct {
	TargetRef  string         `json:"target_ref"`
	ActionName string         `json:"action_name"`
	Input      map[string]any `json:"input,omitempty"`
	Reason     string         `json:"reason,omitempty"`
}

type mcpConnectorActionResponse struct {
	Status            string         `json:"status"`
	RequestID         int64          `json:"request_id,omitempty"`
	TargetRef         string         `json:"target_ref"`
	TargetName        string         `json:"target_name,omitempty"`
	ConnectorKind     string         `json:"connector_kind"`
	ProfileLabel      string         `json:"profile_label,omitempty"`
	ActionName        string         `json:"action_name"`
	Input             map[string]any `json:"input,omitempty"`
	Output            any            `json:"output,omitempty"`
	DisplayText       string         `json:"display_text,omitempty"`
	Error             string         `json:"error,omitempty"`
	RetryAfterSeconds int            `json:"retry_after_seconds,omitempty"`
	AssistantHint     string         `json:"assistant_hint,omitempty"`
}

func (s mcpHandlers) mcpListConnectorTargets(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	permissions, err := connectortargets.NewStore(auth.runtime.database).ListActionPermissions(r.Context(), auth.TokenID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
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
				TargetID:      permission.TargetID,
				TargetName:    permission.TargetName,
				ConnectorKind: permission.ConnectorKind,
				ProfileID:     permission.ProfileID,
				ProfileLabel:  permission.ProfileLabel,
				ProfileKind:   permission.ProfileKind,
				Hints:         connectorTargetHints(permission.ConnectorKind),
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
	target, _, connector, ok := s.resolveMCPConnectorTarget(w, r, auth)
	if !ok {
		return
	}
	actions, err := connector.GetActionList(r.Context(), target)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": actions})
}

func (s mcpHandlers) mcpCallConnectorAction(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	if s.rejectStoppedMCP(w, auth.runtime) {
		return
	}
	var request mcpConnectorActionCallRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.TargetRef = strings.TrimSpace(request.TargetRef)
	request.ActionName = strings.TrimSpace(request.ActionName)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.TargetRef == "" {
		writeError(w, http.StatusBadRequest, "target_ref is required")
		return
	}
	if !connectors.ValidIdentifier(request.ActionName) {
		writeError(w, http.StatusBadRequest, "invalid action_name")
		return
	}
	if err := validateTextLimit("reason", request.Reason, maxReasonBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.callConnectorAction(r.Context(), auth.runtime, connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    auth.TokenID,
		TargetRef:  request.TargetRef,
		ActionName: request.ActionName,
		Input:      request.Input,
		Reason:     request.Reason,
	})
	if err != nil {
		if errors.Is(err, connectortargets.ErrInvalidTargetRef) || errors.Is(err, connectortargets.ErrTargetProfileNotFound) {
			handleConnectorTargetError(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeAudit(r.Context(), auth.runtime, "mcp", int64Ptr(auth.TokenID), 0, "mcp.connector_action."+string(result.Result.Status), map[string]any{
		"request_id":     result.Request.ID,
		"target_ref":     request.TargetRef,
		"connector_kind": result.Request.ConnectorKind,
		"action_name":    request.ActionName,
	})
	writeJSON(w, http.StatusOK, connectorActionToMCPResponse(result.Request, result.Result))
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
	writeJSON(w, http.StatusOK, connectorActionRequestToMCPResponse(request))
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
	permissions, err := connectortargets.NewStore(auth.runtime.database).ListActionPermissions(r.Context(), auth.TokenID)
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
	registry, err := builtin.NewRegistry()
	if err != nil {
		writeInternalError(w)
		return connectors.TargetView{}, connectors.CredentialProfileView{}, nil, false
	}
	connector, ok := registry.Get(target.ConnectorKind)
	if !ok {
		writeError(w, http.StatusNotFound, "connector not found")
		return connectors.TargetView{}, connectors.CredentialProfileView{}, nil, false
	}
	return target, profile, connector, true
}

func connectorActionToMCPResponse(request connectortargets.ActionRequest, result connectors.ActionResult) mcpConnectorActionResponse {
	response := connectorActionRequestToMCPResponse(request)
	response.Output = result.Output
	if result.DisplayText != "" {
		response.DisplayText = result.DisplayText
	}
	if result.Error != "" {
		response.Error = result.Error
	}
	return response
}

func connectorActionRequestToMCPResponse(request connectortargets.ActionRequest) mcpConnectorActionResponse {
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
	}
	if request.Status == connectors.ResultApprovalPending {
		response.RetryAfterSeconds = 3
		response.AssistantHint = connectorActionApprovalHint
	}
	return response
}

func connectorTargetHints(connectorKind string) []string {
	switch connectorKind {
	case "postgres":
		return []string{
			"Use get_connector_help and get_connector_actions before calling a Postgres action for the first time.",
			"Prefer get_schemas, get_tables, and describe_table before writing SQL.",
			"query_readonly is bounded and read-only, but the selected database credential profile still defines the real database permissions.",
		}
	default:
		return []string{"Use get_connector_help and get_connector_actions before calling connector actions."}
	}
}
