package mcpconnector

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type ActionGrant struct {
	Name          string `json:"name"`
	ExecutionRule string `json:"execution_rule"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

type Permission struct {
	ProjectID     int64
	ProjectName   string
	ProjectSlug   string
	TargetID      int64
	TargetName    string
	ProfileID     int64
	ProfileLabel  string
	ConnectorKind string
	ProfileKind   string
	ActionName    string
	ExecutionRule connectortargets.ActionPermissionRule
	ExpiresAt     string
}

type TargetItem struct {
	TargetRef     string         `json:"target_ref"`
	ProjectID     int64          `json:"project_id"`
	ProjectName   string         `json:"project_name"`
	ProjectSlug   string         `json:"project_slug"`
	TargetID      int64          `json:"target_id"`
	TargetName    string         `json:"target_name"`
	ConnectorKind string         `json:"connector_kind"`
	ProfileID     int64          `json:"profile_id"`
	ProfileLabel  string         `json:"profile_label"`
	ProfileKind   string         `json:"profile_kind"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Actions       []ActionGrant  `json:"actions"`
	Hints         []string       `json:"hints,omitempty"`
}

type Scope struct {
	Database        *sql.DB
	Registry        *connectors.Registry
	TokenID         int64
	Permissions     func(context.Context) ([]Permission, error)
	MetadataEnabled func(context.Context) (bool, error)
	Metadata        func(connectors.TargetView, connectors.CredentialProfileView) map[string]any
}

type ScopeProvider func(http.ResponseWriter, *http.Request) (Scope, bool)

type HTTPHandlers struct{ scope ScopeProvider }

func NewHTTPHandlers(scope ScopeProvider) *HTTPHandlers { return &HTTPHandlers{scope: scope} }

func (h *HTTPHandlers) ListTargets(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, r)
	if !ok {
		return
	}
	permissions, err := scope.Permissions(r.Context())
	if err != nil {
		writeTargetError(w, err)
		return
	}
	store := connectortargets.NewStore(scope.Database)
	exposeMetadata, err := scope.MetadataEnabled(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	itemsByRef := map[string]*TargetItem{}
	order := make([]string, 0)
	for _, permission := range permissions {
		if permission.ExecutionRule == connectortargets.ActionPermissionBlocked {
			continue
		}
		ref := connectors.FormatTargetRef(permission.ConnectorKind, permission.TargetID, permission.ProfileID)
		item := itemsByRef[ref]
		if item == nil {
			item = &TargetItem{
				TargetRef: ref, ProjectID: permission.ProjectID, ProjectName: permission.ProjectName,
				ProjectSlug: permission.ProjectSlug, TargetID: permission.TargetID, TargetName: permission.TargetName,
				ConnectorKind: permission.ConnectorKind, ProfileID: permission.ProfileID,
				ProfileLabel: permission.ProfileLabel, ProfileKind: permission.ProfileKind, Hints: targetHints(),
			}
			if exposeMetadata {
				target, profile, err := store.ResolveTargetProfileViews(r.Context(), permission.TargetID, permission.ProfileID)
				if err != nil {
					writeTargetError(w, err)
					return
				}
				item.Metadata = scope.Metadata(target, profile)
			}
			itemsByRef[ref] = item
			order = append(order, ref)
		}
		item.Actions = append(item.Actions, ActionGrant{
			Name: permission.ActionName, ExecutionRule: string(permission.ExecutionRule), ExpiresAt: permission.ExpiresAt,
		})
	}
	items := make([]TargetItem, 0, len(order))
	for _, ref := range order {
		items = append(items, *itemsByRef[ref])
	}
	httptransport.WriteJSON(w, http.StatusOK, items)
}

func (h *HTTPHandlers) GetHelp(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, r)
	if !ok {
		return
	}
	target, _, connector, ok := resolveTarget(w, r, scope)
	if !ok {
		return
	}
	help, err := connector.GetHelp(r.Context(), target)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, help)
}

func (h *HTTPHandlers) GetActions(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, r)
	if !ok {
		return
	}
	target, profile, connector, ok := resolveTarget(w, r, scope)
	if !ok {
		return
	}
	definitions, err := connectors.GetActionDefinitions(r.Context(), connector, target, profile)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	allowed, err := permittedActions(r, scope, target.ID, profile.ID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	filtered := make([]connectors.ActionDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if allowed[definition.Name] {
			filtered = append(filtered, definition)
		}
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": filtered})
}

func (h *HTTPHandlers) resolve(w http.ResponseWriter, r *http.Request) (Scope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return Scope{}, false
	}
	scope, ok := h.scope(w, r)
	if !ok {
		return Scope{}, false
	}
	if scope.Database == nil || scope.Registry == nil || scope.TokenID < 1 || scope.Permissions == nil ||
		scope.MetadataEnabled == nil || scope.Metadata == nil {
		httptransport.WriteInternalError(w)
		return Scope{}, false
	}
	return scope, true
}

func resolveTarget(w http.ResponseWriter, r *http.Request, scope Scope) (connectors.TargetView, connectors.CredentialProfileView, connectors.Connector, bool) {
	targetRef := strings.TrimSpace(r.URL.Query().Get("target_ref"))
	if targetRef == "" {
		httptransport.WriteError(w, http.StatusBadRequest, "target_ref is required")
		return connectors.TargetView{}, connectors.CredentialProfileView{}, nil, false
	}
	target, profile, err := connectortargets.NewStore(scope.Database).ResolveConnectorActionTarget(r.Context(), targetRef)
	if err != nil {
		writeTargetError(w, err)
		return connectors.TargetView{}, connectors.CredentialProfileView{}, nil, false
	}
	permissions, err := scope.Permissions(r.Context())
	if err != nil {
		writeTargetError(w, err)
		return connectors.TargetView{}, connectors.CredentialProfileView{}, nil, false
	}
	for _, permission := range permissions {
		if permission.TargetID == target.ID && permission.ProfileID == profile.ID && permission.ExecutionRule != connectortargets.ActionPermissionBlocked {
			connector, exists := scope.Registry.Get(target.ConnectorKind)
			if !exists {
				httptransport.WriteError(w, http.StatusNotFound, "connector not found")
				return connectors.TargetView{}, connectors.CredentialProfileView{}, nil, false
			}
			return target, profile, connector, true
		}
	}
	httptransport.WriteError(w, http.StatusForbidden, "token has no active connector actions for this target/profile")
	return connectors.TargetView{}, connectors.CredentialProfileView{}, nil, false
}

func permittedActions(r *http.Request, scope Scope, targetID, profileID int64) (map[string]bool, error) {
	permissions, err := scope.Permissions(r.Context())
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, permission := range permissions {
		if permission.TargetID == targetID && permission.ProfileID == profileID && permission.ExecutionRule != connectortargets.ActionPermissionBlocked {
			allowed[permission.ActionName] = true
		}
	}
	return allowed, nil
}

func writeTargetError(w http.ResponseWriter, err error) {
	var validation connectortargets.ValidationError
	switch {
	case errors.Is(err, connectortargets.ErrTargetNotFound), errors.Is(err, connectortargets.ErrTargetProfileNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "connector target not found")
	case errors.Is(err, connectortargets.ErrInvalidTargetRef):
		httptransport.WriteError(w, http.StatusBadRequest, "invalid connector target ref")
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, validation.Error())
	default:
		httptransport.WriteInternalError(w)
	}
}

func targetHints() []string {
	return []string{
		"Use get_connector_help and get_connector_actions before calling connector actions for the first time.",
		"Target, credential profile, and token action permission decide what the connector can do; prefer approval_required until the workflow is trusted.",
	}
}
