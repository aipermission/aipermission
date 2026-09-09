package observability

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

const pendingGracePeriod = 30 * time.Second

type HealthSnapshot struct {
	Status              string `json:"status"`
	FailureCount        uint64 `json:"failure_count"`
	LastFailureAt       string `json:"last_failure_at,omitempty"`
	PendingCount        int64  `json:"pending_count"`
	DeadLetterCount     int64  `json:"dead_letter_count"`
	OldestPendingAt     string `json:"oldest_pending_at,omitempty"`
	RetriedEventCount   int64  `json:"retried_event_count"`
	LastDeliveryError   string `json:"last_delivery_error,omitempty"`
	LastDeliveryErrorAt string `json:"last_delivery_error_at,omitempty"`
	LastDeliverySuccess string `json:"last_delivery_success_at,omitempty"`
}

type HealthTracker struct {
	mu            sync.RWMutex
	failureCount  uint64
	lastFailureAt string
}

func (t *HealthTracker) RecordFailure(now time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failureCount++
	t.lastFailureAt = now.UTC().Format(time.RFC3339Nano)
}

func (t *HealthTracker) Snapshot(ctx context.Context, database *sql.DB) HealthSnapshot {
	response := t.memorySnapshot()
	if database == nil {
		return response
	}
	durable, err := (Store{}).Health(ctx, database)
	if err != nil {
		response.Status = "degraded"
		response.LastDeliveryError = err.Error()
		return response
	}
	if durable.FailureCount > int64(response.FailureCount) {
		response.FailureCount = uint64(durable.FailureCount)
	}
	response.PendingCount = durable.PendingCount
	response.DeadLetterCount = durable.DeadLetterCount
	response.OldestPendingAt = durable.OldestPendingAt
	response.RetriedEventCount = durable.RetriedEventCount
	response.LastDeliveryError = durable.LastDeliveryError
	response.LastDeliveryErrorAt = durable.LastDeliveryErrorAt
	response.LastDeliverySuccess = durable.LastDeliverySuccess
	if response.LastFailureAt != "" && !timestampAfter(response.LastFailureAt, durable.LastDeliverySuccess) {
		response.Status = "ok"
	}
	if durable.DeadLetterCount > 0 || pendingBacklogIsStale(durable.OldestPendingAt, time.Now().UTC()) || timestampAfter(durable.LastDeliveryErrorAt, durable.LastDeliverySuccess) {
		response.Status = "degraded"
	}
	return response
}

func (t *HealthTracker) memorySnapshot() HealthSnapshot {
	if t == nil {
		return HealthSnapshot{Status: "ok"}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	status := "ok"
	if t.failureCount > 0 {
		status = "degraded"
	}
	return HealthSnapshot{
		Status:        status,
		FailureCount:  t.failureCount,
		LastFailureAt: t.lastFailureAt,
	}
}

func pendingBacklogIsStale(oldestPendingAt string, now time.Time) bool {
	if oldestPendingAt == "" {
		return false
	}
	oldest, err := time.Parse(time.RFC3339Nano, oldestPendingAt)
	if err != nil {
		return true
	}
	return !oldest.After(now.Add(-pendingGracePeriod))
}

func timestampAfter(value string, baseline string) bool {
	if value == "" {
		return false
	}
	if baseline == "" {
		return true
	}
	valueTime, valueErr := time.Parse(time.RFC3339Nano, value)
	baselineTime, baselineErr := time.Parse(time.RFC3339Nano, baseline)
	if valueErr != nil || baselineErr != nil {
		return value > baseline
	}
	return valueTime.After(baselineTime)
}
