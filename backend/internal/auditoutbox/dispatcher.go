package auditoutbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	defaultBatchSize    = 100
	defaultPollInterval = 2 * time.Second
	maxDeliveryError    = 1000
)

type queuedEvent struct {
	ID              int64
	EventID         string
	ActorType       string
	TokenID         sql.NullInt64
	ProjectID       sql.NullInt64
	RuntimeID       sql.NullInt64
	ConnectorKind   string
	TargetID        sql.NullInt64
	ProfileID       sql.NullInt64
	ActionRequestID sql.NullInt64
	Action          string
	PayloadJSON     string
	OccurredAt      string
}

type Dispatcher struct {
	database     *sql.DB
	batchSize    int
	pollInterval time.Duration
	wake         chan struct{}
	stop         chan struct{}
	done         chan struct{}
	startOnce    sync.Once
	stopOnce     sync.Once
	dispatchMu   sync.Mutex
}

func NewDispatcher(database *sql.DB) *Dispatcher {
	return &Dispatcher{
		database:     database,
		batchSize:    defaultBatchSize,
		pollInterval: defaultPollInterval,
		wake:         make(chan struct{}, 1),
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
}

func (dispatcher *Dispatcher) Start() {
	if dispatcher == nil || dispatcher.database == nil {
		return
	}
	dispatcher.startOnce.Do(func() {
		go dispatcher.run()
		dispatcher.Notify()
	})
}

func (dispatcher *Dispatcher) Stop() {
	if dispatcher == nil {
		return
	}
	dispatcher.stopOnce.Do(func() { close(dispatcher.stop) })
	select {
	case <-dispatcher.done:
	case <-time.After(5 * time.Second):
	}
}

func (dispatcher *Dispatcher) Notify() {
	if dispatcher == nil {
		return
	}
	select {
	case dispatcher.wake <- struct{}{}:
	default:
	}
}

func (dispatcher *Dispatcher) run() {
	defer close(dispatcher.done)
	ticker := time.NewTicker(dispatcher.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-dispatcher.stop:
			return
		case <-dispatcher.wake:
		case <-ticker.C:
		}
		for {
			count, err := dispatcher.DispatchOnce(context.Background())
			if err != nil || count < dispatcher.batchSize {
				break
			}
		}
	}
}

func (dispatcher *Dispatcher) DispatchOnce(ctx context.Context) (int, error) {
	if dispatcher == nil || dispatcher.database == nil {
		return 0, errors.New("audit dispatcher database is unavailable")
	}
	dispatcher.dispatchMu.Lock()
	defer dispatcher.dispatchMu.Unlock()

	tx, err := dispatcher.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin audit projection: %w", err)
	}
	defer tx.Rollback()
	events, err := loadPendingEvents(ctx, tx, dispatcher.batchSize)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, event := range events {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_logs (
				event_id, actor_type, token_id, project_id, runtime_id, connector_kind,
				target_id, profile_id, action_request_id, action, payload_json, created_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(event_id) WHERE event_id IS NOT NULL DO NOTHING`,
			event.EventID, event.ActorType, nullInt64Value(event.TokenID), nullInt64Value(event.ProjectID),
			nullInt64Value(event.RuntimeID), event.ConnectorKind, nullInt64Value(event.TargetID),
			nullInt64Value(event.ProfileID), nullInt64Value(event.ActionRequestID), event.Action,
			event.PayloadJSON, event.OccurredAt,
		); err != nil {
			_ = tx.Rollback()
			return 0, dispatcher.failDelivery(ctx, events, fmt.Errorf("project audit event %s: %w", event.EventID, err))
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE audit_outbox
			SET delivered_at = ?, last_error = '', last_attempt_at = ?
			WHERE id = ? AND delivered_at IS NULL`, now, now, event.ID); err != nil {
			_ = tx.Rollback()
			return 0, dispatcher.failDelivery(ctx, events, fmt.Errorf("mark audit event %s delivered: %w", event.EventID, err))
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE audit_dispatch_state
		SET last_error = '', last_success_at = ?, updated_at = ?
		WHERE id = 1`, now, now); err != nil {
		_ = tx.Rollback()
		return 0, dispatcher.failDelivery(ctx, events, fmt.Errorf("record audit delivery success: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return 0, dispatcher.failDelivery(ctx, events, fmt.Errorf("commit audit projection: %w", err))
	}
	return len(events), nil
}

func loadPendingEvents(ctx context.Context, tx *sql.Tx, limit int) ([]queuedEvent, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, event_id, actor_type, token_id, project_id, runtime_id, connector_kind,
			target_id, profile_id, action_request_id, action, payload_json, occurred_at
		FROM audit_outbox
		WHERE delivered_at IS NULL
		ORDER BY id ASC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("load pending audit events: %w", err)
	}
	defer rows.Close()
	events := make([]queuedEvent, 0, limit)
	for rows.Next() {
		var event queuedEvent
		if err := rows.Scan(
			&event.ID, &event.EventID, &event.ActorType, &event.TokenID, &event.ProjectID,
			&event.RuntimeID, &event.ConnectorKind, &event.TargetID, &event.ProfileID,
			&event.ActionRequestID, &event.Action, &event.PayloadJSON, &event.OccurredAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending audit event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending audit events: %w", err)
	}
	return events, nil
}

func (dispatcher *Dispatcher) failDelivery(ctx context.Context, events []queuedEvent, deliveryErr error) error {
	message := strings.TrimSpace(deliveryErr.Error())
	if len(message) > maxDeliveryError {
		message = message[:maxDeliveryError]
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	ids := make([]any, 0, len(events)+3)
	placeholders := make([]string, 0, len(events))
	for _, event := range events {
		placeholders = append(placeholders, "?")
		ids = append(ids, event.ID)
	}
	if len(ids) > 0 {
		args := []any{message, now}
		args = append(args, ids...)
		_, _ = dispatcher.database.ExecContext(ctx, `
			UPDATE audit_outbox
			SET attempt_count = attempt_count + 1, last_error = ?, last_attempt_at = ?
			WHERE delivered_at IS NULL AND id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	}
	_, _ = dispatcher.database.ExecContext(ctx, `
		UPDATE audit_dispatch_state
		SET failure_count = failure_count + 1, last_error = ?, last_failure_at = ?, updated_at = ?
		WHERE id = 1`, message, now, now)
	return deliveryErr
}

func nullInt64Value(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}
