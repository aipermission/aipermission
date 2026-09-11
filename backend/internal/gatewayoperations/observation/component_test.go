package observation

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

func TestRequiredAuditFailureDegradesHealth(t *testing.T) {
	component := New()
	if err := component.WriteRequired(t.Context(), nil, "user", nil, 0, "test.audit", map[string]any{"ok": true}); err == nil {
		t.Fatal("required audit write without runtime succeeded")
	}
	health := component.HealthSnapshot(t.Context(), nil)
	if health.Status != "degraded" || health.FailureCount != 1 {
		t.Fatalf("health after failed audit = %+v", health)
	}
}

func TestHTTPHandlersShareActiveRuntimeBoundary(t *testing.T) {
	calls := 0
	handlers := New().HTTPHandlers(func(w http.ResponseWriter) (workspaceruntime.Port, bool) {
		calls++
		http.Error(w, "locked", http.StatusLocked)
		return nil, false
	})
	if handlers.Retention == nil || handlers.History == nil || handlers.Audit == nil {
		t.Fatal("observation handler group is incomplete")
	}
	recorder := httptest.NewRecorder()
	handlers.Audit.List(recorder, httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil))
	if calls != 1 || recorder.Code != http.StatusLocked {
		t.Fatalf("active runtime calls=%d status=%d", calls, recorder.Code)
	}
}
