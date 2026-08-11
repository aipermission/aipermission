package api

import (
	"sync"
	"time"
)

type auditHealthState struct {
	mu            sync.RWMutex
	failureCount  uint64
	lastFailureAt string
}

type auditHealthResponse struct {
	Status        string `json:"status"`
	FailureCount  uint64 `json:"failure_count"`
	LastFailureAt string `json:"last_failure_at,omitempty"`
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
