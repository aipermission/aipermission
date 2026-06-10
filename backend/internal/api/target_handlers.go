package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type targetProfileItem struct {
	Ref           string         `json:"ref"`
	ConnectorKind string         `json:"connector_kind"`
	TargetID      int64          `json:"target_id"`
	TargetName    string         `json:"target_name"`
	ProfileID     int64          `json:"profile_id"`
	ProfileKind   string         `json:"profile_kind"`
	ProfileLabel  string         `json:"profile_label"`
	ServerID      int64          `json:"server_id,omitempty"`
	Config        map[string]any `json:"config,omitempty"`
	Public        map[string]any `json:"public,omitempty"`
	Status        string         `json:"status"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
}

func (s targetHandlers) listTargets(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	rows, err := runtime.database.QueryContext(r.Context(), `
		SELECT
			t.id, t.connector_kind, t.name, t.config_json, t.status,
			p.id, p.kind, p.label, p.public_json,
			COALESCE(r.server_id, 0),
			t.created_at, t.updated_at
		FROM connector_targets t
		JOIN connector_credential_profiles p ON p.target_id = t.id
		LEFT JOIN ssh_connector_profile_runtimes r ON r.target_id = t.id AND r.profile_id = p.id
		WHERE t.status = 'active'
		ORDER BY t.connector_kind, t.name, p.label, p.id`)
	if err != nil {
		writeInternalError(w)
		return
	}
	defer rows.Close()

	items := []targetProfileItem{}
	for rows.Next() {
		var item targetProfileItem
		var configJSON string
		var publicJSON string
		if err := rows.Scan(
			&item.TargetID,
			&item.ConnectorKind,
			&item.TargetName,
			&configJSON,
			&item.Status,
			&item.ProfileID,
			&item.ProfileKind,
			&item.ProfileLabel,
			&publicJSON,
			&item.ServerID,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			writeInternalError(w)
			return
		}
		item.Ref = connectortargets.ConnectorTargetRef(item.ConnectorKind, item.TargetID, item.ProfileID)
		config, err := decodeTargetObject(configJSON)
		if err != nil {
			writeInternalError(w)
			return
		}
		public, err := decodeTargetObject(publicJSON)
		if err != nil {
			writeInternalError(w)
			return
		}
		item.Config = config
		item.Public = public
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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
