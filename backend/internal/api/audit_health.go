package api

import (
	"context"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/auditoutbox"
)

type auditHealthState struct {
	mu            sync.RWMutex
	failureCount  uint64
	lastFailureAt string
}

type auditHealthResponse struct {
	Status              string `json:"status"`
	FailureCount        uint64 `json:"failure_count"`
	LastFailureAt       string `json:"last_failure_at,omitempty"`
	PendingCount        int64  `json:"pending_count"`
	OldestPendingAt     string `json:"oldest_pending_at,omitempty"`
	RetriedEventCount   int64  `json:"retried_event_count"`
	LastDeliveryError   string `json:"last_delivery_error,omitempty"`
	LastDeliveryErrorAt string `json:"last_delivery_error_at,omitempty"`
	LastDeliverySuccess string `json:"last_delivery_success_at,omitempty"`
}

func (s *Server) auditHealthSnapshot(ctx context.Context) auditHealthResponse {
	response := s.auditHealth.snapshot()
	runtime := s.activeRuntime()
	if runtime == nil || runtime.database == nil {
		return response
	}
	durable, err := (auditoutbox.Store{}).Health(ctx, runtime.database)
	if err != nil {
		response.Status = "degraded"
		response.LastDeliveryError = err.Error()
		return response
	}
	response.FailureCount += uint64(durable.FailureCount)
	response.PendingCount = durable.PendingCount
	response.OldestPendingAt = durable.OldestPendingAt
	response.RetriedEventCount = durable.RetriedEventCount
	response.LastDeliveryError = durable.LastDeliveryError
	response.LastDeliveryErrorAt = durable.LastDeliveryErrorAt
	response.LastDeliverySuccess = durable.LastDeliverySuccess
	if response.LastFailureAt != "" && !timestampAfter(response.LastFailureAt, durable.LastDeliverySuccess) {
		response.Status = "ok"
	}
	if durable.PendingCount > 0 || timestampAfter(durable.LastDeliveryErrorAt, durable.LastDeliverySuccess) {
		response.Status = "degraded"
	}
	return response
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

func (state *auditHealthState) recordFailure(now time.Time) {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.failureCount++
	state.lastFailureAt = now.UTC().Format(time.RFC3339)
}

func (state *auditHealthState) snapshot() auditHealthResponse {
	if state == nil {
		return auditHealthResponse{Status: "ok"}
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	status := "ok"
	if state.failureCount > 0 {
		status = "degraded"
	}
	return auditHealthResponse{
		Status:        status,
		FailureCount:  state.failureCount,
		LastFailureAt: state.lastFailureAt,
	}
}
