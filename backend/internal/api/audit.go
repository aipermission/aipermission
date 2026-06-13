package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type auditLogRecord struct {
	ID              int64  `json:"id"`
	ActorType       string `json:"actor_type"`
	TokenID         *int64 `json:"token_id,omitempty"`
	TokenName       string `json:"token_name,omitempty"`
	ServerID        *int64 `json:"server_id,omitempty"`
	ServerName      string `json:"server_name,omitempty"`
	ConnectorKind   string `json:"connector_kind,omitempty"`
	TargetID        *int64 `json:"target_id,omitempty"`
	TargetName      string `json:"target_name,omitempty"`
	ProfileID       *int64 `json:"profile_id,omitempty"`
	ActionRequestID *int64 `json:"action_request_id,omitempty"`
	Action          string `json:"action"`
	PayloadJSON     string `json:"payload_json"`
	CreatedAt       string `json:"created_at"`
}

func (s auditHandlers) listAuditLogs(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	page, err := parsePageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	actor := strings.TrimSpace(r.URL.Query().Get("actor"))
	where := []string{"(? = '' OR a.actor_type = ?)", "(? = 0 OR a.server_id = ?)"}
	args := []any{actor, actor}
	var serverID int64
	if rawServerID := strings.TrimSpace(r.URL.Query().Get("server_id")); rawServerID != "" {
		id, ok := parseInt64Query(w, rawServerID, "server_id")
		if !ok {
			return
		}
		serverID = id
	}
	args = append(args, serverID, serverID)
	connectorKind := strings.TrimSpace(r.URL.Query().Get("connector_kind"))
	if connectorKind != "" {
		where = append(where, "a.connector_kind = ?")
		args = append(args, connectorKind)
	}
	var targetID int64
	if rawTargetID := strings.TrimSpace(r.URL.Query().Get("target_id")); rawTargetID != "" {
		id, ok := parseInt64Query(w, rawTargetID, "target_id")
		if !ok {
			return
		}
		targetID = id
		where = append(where, "a.target_id = ?")
		args = append(args, targetID)
	}
	if page.Query != "" {
		like := "%" + page.Query + "%"
		if ftsQuery := buildFTSQuery(page.Query); ftsQuery != "" {
			where = append(where, `(a.id IN (SELECT rowid FROM audit_logs_fts WHERE audit_logs_fts MATCH ?) OR COALESCE(t.name, '') LIKE ? OR COALESCE(profile_ct.name, '') LIKE ? OR COALESCE(ct.name, '') LIKE ?)`)
			args = append(args, ftsQuery, like, like, like)
		} else {
			where = append(where, `(a.action LIKE ? OR a.actor_type LIKE ? OR a.payload_json LIKE ? OR a.connector_kind LIKE ? OR COALESCE(t.name, '') LIKE ? OR COALESCE(profile_ct.name, '') LIKE ? OR COALESCE(ct.name, '') LIKE ?)`)
			args = append(args, like, like, like, like, like, like, like)
		}
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := runtime.database.QueryRowContext(r.Context(), `
		SELECT COUNT(*)
		FROM audit_logs a
		LEFT JOIN api_tokens t ON t.id = a.token_id
		LEFT JOIN connector_credential_profiles profile_cp ON profile_cp.id = a.server_id
		LEFT JOIN connector_targets profile_ct ON profile_ct.id = profile_cp.target_id
		LEFT JOIN connector_targets ct ON ct.id = a.target_id
		WHERE `+whereSQL,
		args...,
	).Scan(&total); err != nil {
		writeInternalError(w)
		return
	}

	queryArgs := append(append([]any{}, args...), page.Limit, page.Offset)
	rows, err := runtime.database.QueryContext(r.Context(), `
		SELECT a.id, a.actor_type, a.token_id, COALESCE(t.name, ''), a.server_id, COALESCE(profile_ct.name, ''),
			a.connector_kind, a.target_id, COALESCE(ct.name, ''), a.profile_id, a.action_request_id,
			a.action, substr(a.payload_json, 1, 500), a.created_at
		FROM audit_logs a
		LEFT JOIN api_tokens t ON t.id = a.token_id
		LEFT JOIN connector_credential_profiles profile_cp ON profile_cp.id = a.server_id
		LEFT JOIN connector_targets profile_ct ON profile_ct.id = profile_cp.target_id
		LEFT JOIN connector_targets ct ON ct.id = a.target_id
		WHERE `+whereSQL+`
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT ? OFFSET ?`,
		queryArgs...,
	)
	if err != nil {
		writeInternalError(w)
		return
	}
	defer rows.Close()

	items := []auditLogRecord{}
	for rows.Next() {
		item, err := scanAuditLog(rows)
		if err != nil {
			writeInternalError(w)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, makePageResponse(items, total, page))
}

func (s auditHandlers) getAuditLog(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	row := runtime.database.QueryRowContext(r.Context(), `
		SELECT a.id, a.actor_type, a.token_id, COALESCE(t.name, ''), a.server_id, COALESCE(profile_ct.name, ''),
			a.connector_kind, a.target_id, COALESCE(ct.name, ''), a.profile_id, a.action_request_id,
			a.action, a.payload_json, a.created_at
		FROM audit_logs a
		LEFT JOIN api_tokens t ON t.id = a.token_id
		LEFT JOIN connector_credential_profiles profile_cp ON profile_cp.id = a.server_id
		LEFT JOIN connector_targets profile_ct ON profile_ct.id = profile_cp.target_id
		LEFT JOIN connector_targets ct ON ct.id = a.target_id
		WHERE a.id = ?`,
		id,
	)
	item, err := scanAuditLog(row)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "audit log not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func scanAuditLog(scanner interface {
	Scan(dest ...any) error
}) (auditLogRecord, error) {
	var item auditLogRecord
	var tokenID sql.NullInt64
	var serverID sql.NullInt64
	var targetID sql.NullInt64
	var profileID sql.NullInt64
	var actionRequestID sql.NullInt64
	if err := scanner.Scan(
		&item.ID,
		&item.ActorType,
		&tokenID,
		&item.TokenName,
		&serverID,
		&item.ServerName,
		&item.ConnectorKind,
		&targetID,
		&item.TargetName,
		&profileID,
		&actionRequestID,
		&item.Action,
		&item.PayloadJSON,
		&item.CreatedAt,
	); err != nil {
		return auditLogRecord{}, err
	}
	if tokenID.Valid {
		item.TokenID = &tokenID.Int64
	}
	if serverID.Valid {
		item.ServerID = &serverID.Int64
	}
	if targetID.Valid {
		item.TargetID = &targetID.Int64
	}
	if profileID.Valid {
		item.ProfileID = &profileID.Int64
	}
	if actionRequestID.Valid {
		item.ActionRequestID = &actionRequestID.Int64
	}
	return item, nil
}

func (s *Server) writeAudit(ctx context.Context, runtime *databaseRuntime, actorType string, tokenID *int64, serverID int64, action string, payload any) {
	if runtime == nil || runtime.database == nil {
		return
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		payloadBytes = []byte(`{}`)
	}
	payloadJSON := s.redactForPersistence(ctx, runtime, string(payloadBytes))
	connectorKind, targetID, profileID, actionRequestID := auditConnectorMetadata(payload)
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = runtime.database.ExecContext(ctx, `
		INSERT INTO audit_logs (
			actor_type, token_id, server_id, connector_kind, target_id, profile_id,
			action_request_id, action, payload_json, created_at
		)
		VALUES (?, ?, NULLIF(?, 0), ?, NULLIF(?, 0), NULLIF(?, 0), NULLIF(?, 0), ?, ?, ?)`,
		actorType,
		nullableInt64(tokenID),
		serverID,
		connectorKind,
		targetID,
		profileID,
		actionRequestID,
		action,
		payloadJSON,
		now,
	)
}

func auditConnectorMetadata(payload any) (string, int64, int64, int64) {
	values, ok := payload.(map[string]any)
	if !ok {
		return "", 0, 0, 0
	}
	connectorKind := strings.TrimSpace(fmt.Sprint(values["connector_kind"]))
	targetID := int64FromAny(values["target_id"])
	profileID := int64FromAny(values["profile_id"])
	actionRequestID := int64FromAny(values["action_request_id"])
	if actionRequestID == 0 {
		actionRequestID = int64FromAny(values["request_id"])
	}
	if (connectorKind == "" || targetID == 0 || profileID == 0) && values["target_ref"] != nil {
		kind, parsedTargetID, parsedProfileID, ok := connectortargets.ParseConnectorTargetRef(fmt.Sprint(values["target_ref"]))
		if ok {
			if connectorKind == "" {
				connectorKind = kind
			}
			if targetID == 0 {
				targetID = parsedTargetID
			}
			if profileID == 0 {
				profileID = parsedProfileID
			}
		}
	}
	return connectorKind, targetID, profileID, actionRequestID
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		var parsed int64
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err == nil {
			return parsed
		}
	}
	return 0
}

func int64Ptr(value int64) *int64 {
	return &value
}
