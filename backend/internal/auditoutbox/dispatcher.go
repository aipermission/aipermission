package auditoutbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	defaultBatchSize    = 100
	defaultPollInterval = 2 * time.Second
	maxDeliveryError    = 1000
	maxDeliveryAttempts = 8
	maxRetryDelay       = 5 * time.Minute
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
	EventVersion    int
	LifecyclePhase  string
	AttemptCount    int
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

	events, err := loadPendingEvents(ctx, dispatcher.database, dispatcher.batchSize)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	delivered := 0
	var firstErr error
	for _, event := range events {
		if err := dispatcher.dispatchEvent(ctx, event); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delivered++
	}
	if firstErr != nil {
		return delivered, firstErr
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := dispatcher.database.ExecContext(ctx, `
		UPDATE audit_dispatch_state
		SET failure_count = 0, last_error = '', last_success_at = ?, updated_at = ?
		WHERE id = 1`, now, now); err != nil {
		return delivered, fmt.Errorf("record audit delivery success: %w", err)
	}
	return delivered, nil
}

func (dispatcher *Dispatcher) dispatchEvent(ctx context.Context, event queuedEvent) error {
	tx, err := dispatcher.database.BeginTx(ctx, nil)
	if err != nil {
		return dispatcher.failDelivery(ctx, event, fmt.Errorf("begin audit projection: %w", err))
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (
			event_id, event_version, actor_type, token_id, project_id, runtime_id, connector_kind,
			target_id, profile_id, action_request_id, action, lifecycle_phase, payload_json, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(event_id) WHERE event_id IS NOT NULL DO NOTHING`,
		event.EventID, event.EventVersion, event.ActorType, nullInt64Value(event.TokenID), nullInt64Value(event.ProjectID),
		nullInt64Value(event.RuntimeID), event.ConnectorKind, nullInt64Value(event.TargetID),
		nullInt64Value(event.ProfileID), nullInt64Value(event.ActionRequestID), event.Action,
		event.LifecyclePhase, event.PayloadJSON, event.OccurredAt,
	); err != nil {
		_ = tx.Rollback()
		return dispatcher.failDelivery(ctx, event, fmt.Errorf("project audit event %s: %w", event.EventID, err))
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE audit_outbox
		SET delivered_at = ?, last_error = '', last_attempt_at = ?, next_attempt_at = NULL
		WHERE id = ? AND delivered_at IS NULL AND dead_lettered_at IS NULL`, now, now, event.ID); err != nil {
		_ = tx.Rollback()
		return dispatcher.failDelivery(ctx, event, fmt.Errorf("mark audit event %s delivered: %w", event.EventID, err))
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return dispatcher.failDelivery(ctx, event, fmt.Errorf("commit audit projection: %w", err))
	}
	return nil
}

func loadPendingEvents(ctx context.Context, database *sql.DB, limit int) ([]queuedEvent, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT id, event_id, actor_type, token_id, project_id, runtime_id, connector_kind,
			target_id, profile_id, action_request_id, action, payload_json, occurred_at,
			event_version, lifecycle_phase, attempt_count
		FROM audit_outbox
		WHERE delivered_at IS NULL AND dead_lettered_at IS NULL
			AND (next_attempt_at IS NULL OR julianday(next_attempt_at) <= julianday('now'))
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
			&event.EventVersion, &event.LifecyclePhase, &event.AttemptCount,
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

func (dispatcher *Dispatcher) failDelivery(ctx context.Context, event queuedEvent, deliveryErr error) error {
	message := strings.TrimSpace(deliveryErr.Error())
	message = truncateUTF8(message, maxDeliveryError)
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	attempts := event.AttemptCount + 1
	var nextAttempt any
	var deadLettered any
	if attempts >= maxDeliveryAttempts {
		deadLettered = now
	} else {
		nextAttempt = nowTime.Add(deliveryRetryDelay(attempts)).Format(time.RFC3339Nano)
	}
	if _, err := dispatcher.database.ExecContext(ctx, `
		UPDATE audit_outbox
		SET attempt_count = attempt_count + 1, last_error = ?, last_attempt_at = ?,
			next_attempt_at = ?, dead_lettered_at = ?
		WHERE id = ? AND delivered_at IS NULL AND dead_lettered_at IS NULL`,
		message, now, nextAttempt, deadLettered, event.ID,
	); err != nil {
		return fmt.Errorf("%v; record audit delivery retry: %w", deliveryErr, err)
	}
	if _, err := dispatcher.database.ExecContext(ctx, `
		UPDATE audit_dispatch_state
		SET failure_count = failure_count + 1, last_error = ?, last_failure_at = ?, updated_at = ?
		WHERE id = 1`, message, now, now); err != nil {
		return fmt.Errorf("%v; record audit dispatcher failure: %w", deliveryErr, err)
	}
	return deliveryErr
}

func deliveryRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := defaultPollInterval * time.Duration(1<<min(attempt-1, 8))
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func nullInt64Value(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}
