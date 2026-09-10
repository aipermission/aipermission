package connectormanagement

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func (h *HTTPHandlers) ListTargetProfiles(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireDatabase|requireFeatures)
	if !ok {
		return
	}
	rows, err := scope.Database.QueryContext(r.Context(), `
			SELECT
				t.id, t.project_id, project.name, project.slug, t.connector_kind, t.name, t.config_json, t.status,
				p.id, p.kind, p.label, p.public_json,
				t.created_at, t.updated_at
			FROM connector_targets t
			JOIN projects project ON project.id = t.project_id AND project.status = 'active'
			JOIN connector_credential_profiles p ON p.target_id = t.id
			WHERE t.status = 'active' AND p.status = 'active' AND p.connector_kind = t.connector_kind
			ORDER BY lower(project.name), t.connector_kind, lower(t.name), p.label, p.id`)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	defer rows.Close()

	items := []TargetProfileItem{}
	for rows.Next() {
		var item TargetProfileItem
		var configJSON string
		var publicJSON string
		if err := rows.Scan(
			&item.TargetID,
			&item.ProjectID,
			&item.ProjectName,
			&item.ProjectSlug,
			&item.ConnectorKind,
			&item.TargetName,
			&configJSON,
			&item.Status,
			&item.ProfileID,
			&item.ProfileKind,
			&item.ProfileLabel,
			&publicJSON,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			httptransport.WriteInternalError(w)
			return
		}
		item.Ref = connectors.FormatTargetRef(item.ConnectorKind, item.TargetID, item.ProfileID)
		config, err := decodeTargetObject(configJSON)
		if err != nil {
			httptransport.WriteInternalError(w)
			return
		}
		public, err := decodeTargetObject(publicJSON)
		if err != nil {
			httptransport.WriteInternalError(w)
			return
		}
		item.Config = config
		item.Public = public
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		httptransport.WriteInternalError(w)
		return
	}

	store := connectortargets.NewStore(scope.Database)
	for index := range items {
		item := &items[index]
		features := scope.Features(item.ConnectorKind)
		if features.LiveConsoleCapability != "" {
			surface, err := store.GetRuntimeSurfaceByProfile(r.Context(), item.ConnectorKind, item.TargetID, item.ProfileID, features.LiveConsoleCapability)
			if err != nil && !errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
				httptransport.WriteInternalError(w)
				return
			}
			if err == nil {
				item.RuntimeID = surface.ID
			}
		}
		if features.FileTransfer {
			surface, err := store.GetRuntimeSurfaceByProfile(r.Context(), item.ConnectorKind, item.TargetID, item.ProfileID, connectortargets.RuntimeCapabilityFileTransfer)
			if err != nil && !errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
				httptransport.WriteInternalError(w)
				return
			}
			if err == nil {
				item.TransferRuntimeID = surface.ID
			}
		}
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func decodeTargetObject(value string) (map[string]any, error) {
	if value == "" {
		return map[string]any{}, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return nil, fmt.Errorf("decode target json: %w", err)
	}
	if parsed == nil {
		parsed = map[string]any{}
	}
	return parsed, nil
}
