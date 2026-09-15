package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type BuildInput struct {
	ActorType string
	TokenID   *int64
	RuntimeID int64
	Action    string
	Payload   any
	Redact    func(string) string
}

func BuildEvent(ctx context.Context, executor sqldb.Executor, input BuildInput) (Event, error) {
	payloadBytes, err := json.Marshal(input.Payload)
	if err != nil {
		return Event{}, fmt.Errorf("marshal audit payload: %w", err)
	}
	payloadJSON := string(payloadBytes)
	if input.Redact != nil {
		payloadJSON = input.Redact(payloadJSON)
	}
	connectorKind, projectID, targetID, profileID, actionRequestID := connectorMetadata(input.Payload)
	projectID = resolveProjectID(ctx, executor, projectID, connectorKind, actionRequestID, targetID, input.RuntimeID)
	return Event{
		ActorType:       input.ActorType,
		TokenID:         input.TokenID,
		ProjectID:       projectID,
		RuntimeID:       input.RuntimeID,
		ConnectorKind:   connectorKind,
		TargetID:        targetID,
		ProfileID:       profileID,
		ActionRequestID: actionRequestID,
		Action:          input.Action,
		LifecyclePhase:  lifecyclePhase(input.Action),
		PayloadJSON:     payloadJSON,
	}, nil
}

func lifecyclePhase(action string) string {
	action = strings.TrimSpace(action)
	if index := strings.LastIndexByte(action, '.'); index >= 0 && index+1 < len(action) {
		switch phase := action[index+1:]; phase {
		case "requested", "approval_pending", "started", "running", "completed", "failed", "declined", "canceled", "stale", "expired", "blocked", "outcome_unknown", "updated", "created", "deleted", "archived", "closed", "connected", "connecting", "paused", "pending":
			return phase
		}
	}
	return "observed"
}

func connectorMetadata(payload any) (string, int64, int64, int64, int64) {
	values, ok := payload.(map[string]any)
	if !ok {
		return "", 0, 0, 0, 0
	}
	connectorKind := stringFromAny(values["connector_kind"])
	projectID := int64FromAny(values["project_id"])
	targetID := int64FromAny(values["target_id"])
	profileID := int64FromAny(values["profile_id"])
	actionRequestID := int64FromAny(values["action_request_id"])
	if actionRequestID == 0 {
		actionRequestID = int64FromAny(values["request_id"])
	}
	if (connectorKind == "" || targetID == 0 || profileID == 0) && values["target_ref"] != nil {
		kind, parsedTargetID, parsedProfileID, ok := connectors.ParseTargetRef(fmt.Sprint(values["target_ref"]))
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
	return connectorKind, projectID, targetID, profileID, actionRequestID
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func resolveProjectID(ctx context.Context, executor sqldb.Executor, projectID int64, connectorKind string, actionRequestID int64, targetID int64, runtimeID int64) int64 {
	if projectID > 0 || executor == nil {
		return projectID
	}
	if actionRequestID > 0 && targetID > 0 && connectorKind != "" {
		_ = executor.QueryRowContext(ctx, `
			SELECT project_id FROM history_entries
			WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?
				AND connector_kind = ? AND target_id = ?`, actionRequestID, connectorKind, targetID).Scan(&projectID)
		if projectID > 0 {
			return projectID
		}
	}
	if targetID > 0 {
		_ = executor.QueryRowContext(ctx, `SELECT project_id FROM connector_targets WHERE id = ?`, targetID).Scan(&projectID)
		if projectID > 0 {
			return projectID
		}
	}
	if runtimeID > 0 {
		_ = executor.QueryRowContext(ctx, `
			SELECT ct.project_id
			FROM connector_runtime_surfaces rs
			JOIN connector_targets ct ON ct.id = rs.target_id
			WHERE rs.id = ?`, runtimeID).Scan(&projectID)
	}
	return projectID
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
