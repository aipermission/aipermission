package connectortargets

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/history"
)

type RunningActionIdentity struct {
	ID            int64
	TokenID       *int64
	ProjectID     int64
	TargetID      int64
	ProfileID     int64
	ConnectorKind string
	ActionName    string
}

// MarkRunningOutcomeUnknown updates canonical and projected action state
// without decoding payload columns that may themselves be malformed.
func (s *Store) MarkRunningOutcomeUnknown(ctx context.Context, message string, completedAt time.Time) ([]RunningActionIdentity, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.token_id, t.project_id, r.target_id, r.profile_id, r.connector_kind, r.action_name
		FROM connector_action_requests r
		JOIN connector_targets t ON t.id = r.target_id
		WHERE r.status = ?
		ORDER BY r.id`, string(connectors.ResultRunning))
	if err != nil {
		return nil, err
	}
	type storedIdentity struct {
		id            int64
		tokenID       sql.NullInt64
		projectID     int64
		targetID      int64
		profileID     int64
		connectorKind string
		actionName    string
	}
	var stored []storedIdentity
	for rows.Next() {
		var item storedIdentity
		if err := rows.Scan(
			&item.id, &item.tokenID, &item.projectID, &item.targetID, &item.profileID,
			&item.connectorKind, &item.actionName,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		stored = append(stored, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	finishedAt := completedAt.UTC().Format(time.RFC3339Nano)
	identities := make([]RunningActionIdentity, 0, len(stored))
	for _, item := range stored {
		result, err := s.db.ExecContext(ctx, `
			UPDATE connector_action_requests
			SET status = ?, output_json = 'null', display_text = '', error = ?, completed_at = ?
			WHERE id = ? AND status = ?`,
			string(connectors.ResultOutcomeUnknown), strings.TrimSpace(message), finishedAt,
			item.id, string(connectors.ResultRunning),
		)
		if err != nil {
			return nil, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected == 0 {
			continue
		}
		if err := history.SyncConnectorActionRequestWithExecutor(ctx, s.db, item.id); err != nil {
			return nil, err
		}
		identity := RunningActionIdentity{
			ID: item.id, ProjectID: item.projectID, TargetID: item.targetID, ProfileID: item.profileID,
			ConnectorKind: item.connectorKind, ActionName: item.actionName,
		}
		if item.tokenID.Valid {
			value := item.tokenID.Int64
			identity.TokenID = &value
		}
		identities = append(identities, identity)
	}
	return identities, nil
}
