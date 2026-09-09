package auditoutbox

import (
	"sync"
	"testing"
	"time"
)

func TestHealthTrackerStartsClean(t *testing.T) {
	health := (&HealthTracker{}).memorySnapshot()
	if health.Status != "ok" || health.FailureCount != 0 || health.LastFailureAt != "" {
		t.Fatalf("unexpected initial audit health: %+v", health)
	}
}

func TestHealthTrackerCountsConcurrentFailures(t *testing.T) {
	var tracker HealthTracker
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			tracker.RecordFailure(time.Now())
		}()
	}
	wait.Wait()

	health := tracker.memorySnapshot()
	if health.Status != "degraded" || health.FailureCount != 32 {
		t.Fatalf("unexpected concurrent audit health: %+v", health)
	}
}

func TestPendingBacklogStalenessFailsClosed(t *testing.T) {
	now := time.Now().UTC()
	if pendingBacklogIsStale(now.Add(-pendingGracePeriod/2).Format(time.RFC3339Nano), now) {
		t.Fatal("fresh audit backlog should not degrade health")
	}
	if !pendingBacklogIsStale(now.Add(-pendingGracePeriod-time.Second).Format(time.RFC3339Nano), now) {
		t.Fatal("stale audit backlog should degrade health")
	}
	if !pendingBacklogIsStale("invalid timestamp", now) {
		t.Fatal("invalid audit backlog timestamp should fail closed")
	}
}
