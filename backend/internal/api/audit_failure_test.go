package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBestEffortAuditWriteReportsFailure(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	server := &Server{}
	server.writeObservationAudit(context.Background(), nil, "user", nil, 17, "test.audit", map[string]any{
		"unsupported": func() {},
	})

	message := output.String()
	for _, expected := range []string{`actor="user"`, "runtime_id=17", `action="test.audit"`, "audit database is unavailable"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("audit failure log %q does not contain %q", message, expected)
		}
	}

	recorder := httptest.NewRecorder()
	server.status(recorder, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	var status struct {
		Audit auditHealthResponse `json:"audit"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Audit.Status != "degraded" || status.Audit.FailureCount != 1 || status.Audit.LastFailureAt == "" {
		t.Fatalf("unexpected audit health: %+v", status.Audit)
	}
}

func TestAuditHealthStartsClean(t *testing.T) {
	health := (&Server{}).auditHealth.snapshot()
	if health.Status != "ok" || health.FailureCount != 0 || health.LastFailureAt != "" {
		t.Fatalf("unexpected initial audit health: %+v", health)
	}
}

func TestAuditHealthCountsConcurrentFailures(t *testing.T) {
	var state auditHealthState
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			state.recordFailure(time.Now())
		}()
	}
	wait.Wait()

	health := state.snapshot()
	if health.Status != "degraded" || health.FailureCount != 32 {
		t.Fatalf("unexpected concurrent audit health: %+v", health)
	}
}

func TestAuditHealthRecoversAfterLaterDurableDelivery(t *testing.T) {
	fixture := newAPITestFixture(t)
	server := fixture.server
	server.auditHealth.recordFailure(time.Now().Add(-time.Minute))
	if _, err := fixture.db.Exec(`
		UPDATE audit_dispatch_state
		SET failure_count = 1, last_error = '',
			last_failure_at = ?, last_success_at = ?, updated_at = ?
		WHERE id = 1`,
		time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatal(err)
	}

	health := server.auditHealthSnapshot(context.Background())
	if health.Status != "ok" || health.FailureCount != 1 || health.LastDeliverySuccess == "" {
		t.Fatalf("unexpected recovered audit health: %+v", health)
	}
}

func TestAuditHealthAllowsFreshPendingDelivery(t *testing.T) {
	now := time.Now().UTC()
	if pendingAuditBacklogIsStale(now.Add(-auditPendingGracePeriod/2).Format(time.RFC3339Nano), now) {
		t.Fatal("fresh audit backlog should not degrade health")
	}
	if !pendingAuditBacklogIsStale(now.Add(-auditPendingGracePeriod-time.Second).Format(time.RFC3339Nano), now) {
		t.Fatal("stale audit backlog should degrade health")
	}
	if !pendingAuditBacklogIsStale("invalid timestamp", now) {
		t.Fatal("invalid audit backlog timestamp should fail closed")
	}
}
