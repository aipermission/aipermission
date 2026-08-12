package auditoutbox

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	EventVersion    = 1
	MaxPayloadBytes = 64 * 1024
)

type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Event struct {
	EventID         string
	EventVersion    int
	ActorType       string
	TokenID         *int64
	ProjectID       int64
	RuntimeID       int64
	ConnectorKind   string
	TargetID        int64
	ProfileID       int64
	ActionRequestID int64
	Action          string
	LifecyclePhase  string
	PayloadJSON     string
	OccurredAt      time.Time
}

type Health struct {
	PendingCount        int64
	DeadLetterCount     int64
	OldestPendingAt     string
	RetriedEventCount   int64
	FailureCount        int64
	LastDeliveryError   string
	LastDeliveryErrorAt string
	LastDeliverySuccess string
}

type Store struct{}

func NewEventID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate audit event id: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func (Store) Append(ctx context.Context, executor DBTX, event Event) (Event, error) {
	if executor == nil {
		return Event{}, errors.New("audit outbox executor is unavailable")
	}
	event.ActorType = strings.TrimSpace(event.ActorType)
	event.Action = strings.TrimSpace(event.Action)
	if event.ActorType == "" || event.Action == "" {
		return Event{}, errors.New("audit actor and action are required")
	}
	if len(event.PayloadJSON) > MaxPayloadBytes {
		return Event{}, fmt.Errorf("audit payload exceeds %d bytes", MaxPayloadBytes)
	}
	if !json.Valid([]byte(event.PayloadJSON)) {
		return Event{}, errors.New("audit payload must be JSON")
	}
	if event.EventID == "" {
		var err error
		event.EventID, err = NewEventID()
		if err != nil {
			return Event{}, err
		}
	}
	if event.EventVersion == 0 {
		event.EventVersion = EventVersion
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := executor.ExecContext(ctx, `
		INSERT INTO audit_outbox (
			event_id, event_version, actor_type, token_id, project_id, runtime_id,
			connector_kind, target_id, profile_id, action_request_id, action,
			lifecycle_phase, payload_json, occurred_at, created_at
		)
		VALUES (?, ?, ?, ?, NULLIF(?, 0), NULLIF(?, 0), ?, NULLIF(?, 0),
			NULLIF(?, 0), NULLIF(?, 0), ?, ?, ?, ?, ?)`,
		event.EventID, event.EventVersion, event.ActorType, nullableInt64(event.TokenID),
		event.ProjectID, event.RuntimeID, event.ConnectorKind, event.TargetID,
		event.ProfileID, event.ActionRequestID, event.Action, event.LifecyclePhase,
		event.PayloadJSON, event.OccurredAt.UTC().Format(time.RFC3339Nano), now,
	)
	if err != nil {
		return Event{}, fmt.Errorf("append audit outbox event: %w", err)
	}
	return event, nil
}

func (Store) Health(ctx context.Context, database *sql.DB) (Health, error) {
	if database == nil {
		return Health{}, errors.New("audit database is unavailable")
	}
	var health Health
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MIN(created_at), ''),
			COALESCE(SUM(CASE WHEN attempt_count > 0 THEN 1 ELSE 0 END), 0)
		FROM audit_outbox
		WHERE delivered_at IS NULL AND dead_lettered_at IS NULL`).Scan(&health.PendingCount, &health.OldestPendingAt, &health.RetriedEventCount); err != nil {
		return Health{}, fmt.Errorf("read audit outbox backlog: %w", err)
	}
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM audit_outbox
		WHERE delivered_at IS NULL AND dead_lettered_at IS NOT NULL`).Scan(&health.DeadLetterCount); err != nil {
		return Health{}, fmt.Errorf("read audit dead-letter count: %w", err)
	}
	if err := database.QueryRowContext(ctx, `
		SELECT failure_count, last_error, COALESCE(last_failure_at, ''), COALESCE(last_success_at, '')
		FROM audit_dispatch_state WHERE id = 1`).Scan(
		&health.FailureCount, &health.LastDeliveryError, &health.LastDeliveryErrorAt, &health.LastDeliverySuccess,
	); err != nil {
		return Health{}, fmt.Errorf("read audit dispatcher state: %w", err)
	}
	return health, nil
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
