package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
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
	permissions, err := activeSupportedConnectorPermissions(r.Context(), runtime, tokenID)
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
	inputs, err := connectorPermissionInputs(r, runtime.connectorRegistry(), store, request.Permissions)
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

func connectorPermissionInputs(r *http.Request, registry *connectors.Registry, store *connectortargets.Store, permissions []connectorPermissionInput) ([]connectortargets.SetActionPermissionInput, error) {
	inputs := make([]connectortargets.SetActionPermissionInput, 0, len(permissions))
	for _, permission := range permissions {
		target, profile, err := connectorTargetProfileViews(r.Context(), store, permission.TargetID, permission.ProfileID)
		if err != nil {
			return nil, err
		}
		connector, ok := registry.Get(target.ConnectorKind)
		if !ok {
			return nil, connectortargets.ValidationError("unsupported connector kind")
		}
		actionName := strings.TrimSpace(permission.ActionName)
		if !actionSupported(r, connector, target, profile, actionName) {
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

func activeSupportedConnectorPermissions(ctx context.Context, runtime *databaseRuntime, tokenID int64) ([]connectortargets.ActionPermission, error) {
	if runtime == nil || runtime.database == nil {
		return nil, connectortargets.ValidationError("database runtime is not available")
	}
	store := connectortargets.NewStore(runtime.database)
	permissions, err := store.ListActionPermissions(ctx, tokenID)
	if err != nil {
		return nil, err
	}
	registry := runtime.connectorRegistry()
	type actionCatalog struct {
		names map[string]bool
		skip  bool
		err   error
	}
	catalogs := map[string]actionCatalog{}
	supported := make([]connectortargets.ActionPermission, 0, len(permissions))
	for _, permission := range permissions {
		cacheKey := strconv.FormatInt(permission.TargetID, 10) + ":" + strconv.FormatInt(permission.ProfileID, 10)
		catalog, ok := catalogs[cacheKey]
		if !ok {
			catalog = actionCatalog{names: map[string]bool{}}
			target, profile, resolveErr := connectorTargetProfileViews(ctx, store, permission.TargetID, permission.ProfileID)
			if resolveErr != nil {
				if errors.Is(resolveErr, connectortargets.ErrTargetNotFound) || errors.Is(resolveErr, connectortargets.ErrTargetProfileNotFound) {
					catalog.skip = true
				} else {
					catalog.err = resolveErr
				}
			} else {
				connector, exists := registry.Get(target.ConnectorKind)
				if !exists {
					catalog.skip = true
				} else {
					actions, actionsErr := connector.GetActionList(ctx, target, profile)
					if actionsErr != nil {
						catalog.err = actionsErr
					} else if validateErr := connectors.ValidateActionDefinitions(actions, target.ConnectorKind+" actions"); validateErr != nil {
						catalog.err = validateErr
					} else {
						for _, action := range actions {
							catalog.names[action.Name] = true
						}
					}
				}
			}
			catalogs[cacheKey] = catalog
		}
		if catalog.skip {
			continue
		}
		if catalog.err != nil {
			return nil, catalog.err
		}
		if catalog.names[permission.ActionName] {
			supported = append(supported, permission)
		}
	}
	return supported, nil
}

func connectorTargetProfileViews(ctx context.Context, store *connectortargets.Store, targetID int64, profileID int64) (connectors.TargetView, connectors.CredentialProfileView, error) {
	target, err := store.GetTarget(ctx, targetID)
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, err
	}
	profile, err := store.GetCredentialProfile(ctx, targetID, profileID)
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, err
	}
	ref := connectortargets.ConnectorTargetRef(target.ConnectorKind, target.ID, profile.ID)
	return connectors.TargetView{
		ID:            target.ID,
		Ref:           ref,
		ConnectorKind: target.ConnectorKind,
		Name:          target.Name,
		Config:        target.Config,
	}, connectortargets.CredentialProfileView(profile), nil
}

func actionSupported(r *http.Request, connector connectors.Connector, target connectors.TargetView, profile connectors.CredentialProfileView, actionName string) bool {
	if !connectors.ValidIdentifier(actionName) {
		return false
	}
	actions, err := connector.GetActionList(r.Context(), target, profile)
	if err != nil {
		return false
	}
	if err := connectors.ValidateActionDefinitions(actions, target.ConnectorKind+" actions"); err != nil {
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
