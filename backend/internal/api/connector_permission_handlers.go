package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type updateConnectorPermissionsRequest struct {
	Permissions []connectorPermissionInput `json:"permissions"`
}

type connectorPermissionInput struct {
	TargetID      int64  `json:"target_id"`
	ProfileID     int64  `json:"profile_id"`
	ActionName    string `json:"action_name"`
	ExecutionRule string `json:"execution_rule"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

type connectorPermissionResponse struct {
	TargetID      int64  `json:"target_id"`
	TargetName    string `json:"target_name"`
	ProfileID     int64  `json:"profile_id"`
	ProfileLabel  string `json:"profile_label"`
	TargetRef     string `json:"target_ref"`
	ConnectorKind string `json:"connector_kind"`
	ProfileKind   string `json:"profile_kind"`
	ActionName    string `json:"action_name"`
	ExecutionRule string `json:"execution_rule"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func (s tokenHandlers) listTokenConnectorPermissions(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	tokenID, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, err := runtime.tokens.Get(r.Context(), tokenID); err != nil {
		handleTokenError(w, err)
		return
	}
	permissions, err := connectortargets.NewStore(runtime.database).ListActionPermissions(r.Context(), tokenID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": connectorPermissionResponses(permissions)})
}

func (s tokenHandlers) updateTokenConnectorPermissions(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	tokenID, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, err := runtime.tokens.Get(r.Context(), tokenID); err != nil {
		handleTokenError(w, err)
		return
	}
	var request updateConnectorPermissionsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	store := connectortargets.NewStore(runtime.database)
	inputs, err := connectorPermissionInputs(r, store, request.Permissions)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	permissions, err := store.ReplaceActionPermissions(r.Context(), tokenID, inputs)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "token.connector_permissions.updated", map[string]any{
		"token_id":    tokenID,
		"permissions": connectorPermissionResponses(permissions),
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": connectorPermissionResponses(permissions)})
}

func connectorPermissionInputs(r *http.Request, store *connectortargets.Store, permissions []connectorPermissionInput) ([]connectortargets.SetActionPermissionInput, error) {
	registry, err := builtin.NewRegistry()
	if err != nil {
		return nil, err
	}
	inputs := make([]connectortargets.SetActionPermissionInput, 0, len(permissions))
	for _, permission := range permissions {
		target, err := store.GetTarget(r.Context(), permission.TargetID)
		if err != nil {
			return nil, err
		}
		connector, ok := registry.Get(target.ConnectorKind)
		if !ok {
			return nil, connectortargets.ValidationError("unsupported connector kind")
		}
		actionName := strings.TrimSpace(permission.ActionName)
		if !actionSupported(r, connector, actionName) {
			return nil, connectortargets.ValidationError("unsupported connector action")
		}
		expiresAt, err := parseConnectorPermissionExpiresAt(permission.ExpiresAt, permission.ExecutionRule)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, connectortargets.SetActionPermissionInput{
			TargetID:      permission.TargetID,
			ProfileID:     permission.ProfileID,
			ActionName:    actionName,
			ExecutionRule: connectortargets.ActionPermissionRule(permission.ExecutionRule),
			ExpiresAt:     expiresAt,
		})
	}
	return inputs, nil
}

func actionSupported(r *http.Request, connector connectors.Connector, actionName string) bool {
	if !connectors.ValidIdentifier(actionName) {
		return false
	}
	actions, err := connector.GetActionList(r.Context(), connectors.TargetView{ConnectorKind: connector.Kind()})
	if err != nil {
		return false
	}
	for _, action := range actions {
		if action.Name == actionName {
			return true
		}
	}
	return false
}

func parseConnectorPermissionExpiresAt(value string, rule string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if rule == string(connectortargets.ActionPermissionBlocked) {
		return nil, connectortargets.ValidationError("expires_at is not supported for blocked permissions")
	}
	expiresAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, connectortargets.ValidationError("expires_at must be an RFC3339 timestamp")
	}
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(time.Now().UTC()) {
		return nil, connectortargets.ValidationError("expires_at must be in the future")
	}
	return &expiresAt, nil
}

func connectorPermissionResponses(permissions []connectortargets.ActionPermission) []connectorPermissionResponse {
	items := make([]connectorPermissionResponse, 0, len(permissions))
	for _, permission := range permissions {
		items = append(items, connectorPermissionResponse{
			TargetID:      permission.TargetID,
			TargetName:    permission.TargetName,
			ProfileID:     permission.ProfileID,
			ProfileLabel:  permission.ProfileLabel,
			TargetRef:     connectortargets.ConnectorTargetRef(permission.ConnectorKind, permission.TargetID, permission.ProfileID),
			ConnectorKind: permission.ConnectorKind,
			ProfileKind:   permission.ProfileKind,
			ActionName:    permission.ActionName,
			ExecutionRule: string(permission.ExecutionRule),
			ExpiresAt:     permission.ExpiresAt,
			CreatedAt:     permission.CreatedAt,
			UpdatedAt:     permission.UpdatedAt,
		})
	}
	return items
}
