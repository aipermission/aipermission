package accesscontrol

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type UpdateConnectorPermissionsRequest struct {
	Permissions      []ConnectorPermissionInput `json:"permissions"`
	ExpectedRevision string                     `json:"expected_revision"`
}

type ConnectorPermissionInput struct {
	TargetID      int64  `json:"target_id"`
	ProfileID     int64  `json:"profile_id"`
	ActionName    string `json:"action_name"`
	ExecutionRule string `json:"execution_rule"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

type connectorPermissionResponse struct {
	ProjectID      int64  `json:"project_id"`
	ProjectName    string `json:"project_name"`
	ProjectSlug    string `json:"project_slug"`
	ProjectEnabled bool   `json:"project_enabled"`
	TargetID       int64  `json:"target_id"`
	TargetName     string `json:"target_name"`
	ProfileID      int64  `json:"profile_id"`
	ProfileLabel   string `json:"profile_label"`
	TargetRef      string `json:"target_ref"`
	ConnectorKind  string `json:"connector_kind"`
	ProfileKind    string `json:"profile_kind"`
	ActionName     string `json:"action_name"`
	ExecutionRule  string `json:"execution_rule"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func (h *HTTPHandlers) ListConnectorPermissions(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireDatabase|requireTokens|requireRegistry)
	if !ok {
		return
	}
	tokenID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	if _, err := scope.Tokens.Get(r.Context(), tokenID); err != nil {
		writeTokenError(w, err)
		return
	}
	store := connectortargets.NewStore(scope.Database)
	rawPermissions, err := store.ListActionPermissions(r.Context(), tokenID)
	if err != nil {
		writeConnectorTargetError(w, err)
		return
	}
	permissions, err := filterSupportedConnectorPermissions(r.Context(), scope.Database, scope.Registry, rawPermissions)
	if err != nil {
		writeConnectorTargetError(w, err)
		return
	}
	revision, err := connectorPermissionsRevision(rawPermissions)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": connectorPermissionResponses(permissions), "revision": revision})
}

func (h *HTTPHandlers) UpdateConnectorPermissions(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireDatabase|requireTokens|requireRegistry|requireAuthorizationMutation)
	if !ok {
		return
	}
	tokenID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	if _, err := scope.Tokens.Get(r.Context(), tokenID); err != nil {
		writeTokenError(w, err)
		return
	}
	var request UpdateConnectorPermissionsRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	store := connectortargets.NewStore(scope.Database)
	inputs, err := connectorPermissionInputs(r.Context(), scope.Registry, store, request.Permissions)
	if err != nil {
		writeConnectorTargetError(w, err)
		return
	}
	var permissions []connectortargets.ActionPermission
	changed, err := mutateAuthorization(r.Context(), scope, tokenID, "token.connector_permissions.updated", func() any {
		return map[string]any{"token_id": tokenID, "permissions": connectorPermissionResponses(permissions)}
	}, "connector action permission changed; send a fresh request", func(tx *sql.Tx) (bool, error) {
		txStore := connectortargets.NewTxStore(tx)
		current, currentErr := txStore.ListActionPermissions(r.Context(), tokenID)
		if currentErr != nil {
			return false, currentErr
		}
		currentRevision, revisionErr := connectorPermissionsRevision(current)
		if _, revisionErr = requireAuthorizationRevision(request.ExpectedRevision, currentRevision, revisionErr); revisionErr != nil {
			return false, revisionErr
		}
		nextPermissions, mutationChanged, replaceErr := txStore.ReplaceActionPermissionsWithChange(r.Context(), tokenID, inputs)
		permissions = nextPermissions
		return mutationChanged, replaceErr
	})
	if errors.Is(err, ErrVaultDeliveryCanceled) {
		httptransport.WriteError(w, http.StatusRequestTimeout, "connector permission update was canceled")
		return
	}
	if err != nil {
		if writeAuthorizationRevisionError(w, err) {
			return
		}
		writeConnectorTargetError(w, err)
		return
	}
	revision, revisionErr := connectorPermissionsRevision(permissions)
	if revisionErr != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"items": connectorPermissionResponses(permissions), "changed": changed, "revision": revision,
	})
}

func connectorPermissionInputs(ctx context.Context, registry connectors.Catalog, store *connectortargets.Store, permissions []ConnectorPermissionInput) ([]connectortargets.SetActionPermissionInput, error) {
	inputs := make([]connectortargets.SetActionPermissionInput, 0, len(permissions))
	for _, permission := range permissions {
		target, profile, err := store.ResolveTargetProfileViews(ctx, permission.TargetID, permission.ProfileID)
		if err != nil {
			return nil, err
		}
		connector, ok := registry.Get(target.ConnectorKind)
		if !ok {
			return nil, connectortargets.ValidationError("unsupported connector kind")
		}
		actionName := strings.TrimSpace(permission.ActionName)
		if !actionSupported(ctx, connector, target, profile, actionName) {
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

func ActiveSupportedConnectorPermissions(ctx context.Context, database *sql.DB, registry connectors.Catalog, tokenID int64) ([]connectortargets.ActionPermission, error) {
	return supportedConnectorPermissions(ctx, database, registry, tokenID, false)
}

func ProjectScopedSupportedConnectorPermissions(ctx context.Context, database *sql.DB, registry connectors.Catalog, tokenID int64) ([]connectortargets.ActionPermission, error) {
	return supportedConnectorPermissions(ctx, database, registry, tokenID, true)
}

func supportedConnectorPermissions(ctx context.Context, database *sql.DB, registry connectors.Catalog, tokenID int64, projectScoped bool) ([]connectortargets.ActionPermission, error) {
	if database == nil || registry == nil {
		return nil, connectortargets.ValidationError("database runtime is not available")
	}
	store := connectortargets.NewStore(database)
	var permissions []connectortargets.ActionPermission
	var err error
	if projectScoped {
		permissions, err = store.ListScopedActionPermissions(ctx, tokenID, time.Now().UTC())
	} else {
		permissions, err = store.ListActionPermissions(ctx, tokenID)
	}
	if err != nil {
		return nil, err
	}
	return filterSupportedConnectorPermissions(ctx, database, registry, permissions)
}

func filterSupportedConnectorPermissions(ctx context.Context, database *sql.DB, registry connectors.Catalog, permissions []connectortargets.ActionPermission) ([]connectortargets.ActionPermission, error) {
	store := connectortargets.NewStore(database)
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
			target, profile, resolveErr := store.ResolveTargetProfileViews(ctx, permission.TargetID, permission.ProfileID)
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
					actions, actionsErr := connectors.GetActionDefinitions(ctx, connector, target, profile)
					if actionsErr != nil {
						catalog.skip = true
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

func actionSupported(ctx context.Context, connector connectors.Connector, target connectors.TargetView, profile connectors.CredentialProfileView, actionName string) bool {
	if !connectors.ValidIdentifier(actionName) {
		return false
	}
	actions, err := connectors.GetActionDefinitions(ctx, connector, target, profile)
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
			ProjectID:      permission.ProjectID,
			ProjectName:    permission.ProjectName,
			ProjectSlug:    permission.ProjectSlug,
			ProjectEnabled: permission.ProjectEnabled,
			TargetID:       permission.TargetID,
			TargetName:     permission.TargetName,
			ProfileID:      permission.ProfileID,
			ProfileLabel:   permission.ProfileLabel,
			TargetRef:      connectors.FormatTargetRef(permission.ConnectorKind, permission.TargetID, permission.ProfileID),
			ConnectorKind:  permission.ConnectorKind,
			ProfileKind:    permission.ProfileKind,
			ActionName:     permission.ActionName,
			ExecutionRule:  string(permission.ExecutionRule),
			ExpiresAt:      permission.ExpiresAt,
			CreatedAt:      permission.CreatedAt,
			UpdatedAt:      permission.UpdatedAt,
		})
	}
	return items
}

func writeConnectorTargetError(w http.ResponseWriter, err error) {
	var validation connectortargets.ValidationError
	switch {
	case errors.Is(err, connectortargets.ErrTargetUpdateConflict),
		errors.Is(err, connectortargets.ErrCredentialProfileUpdateConflict):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, connectortargets.ErrTargetNotFound),
		errors.Is(err, connectortargets.ErrTargetProfileNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "connector target not found")
	case errors.Is(err, connectortargets.ErrInvalidTargetRef):
		httptransport.WriteError(w, http.StatusBadRequest, "invalid connector target ref")
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, validation.Error())
	default:
		httptransport.WriteInternalError(w)
	}
}
