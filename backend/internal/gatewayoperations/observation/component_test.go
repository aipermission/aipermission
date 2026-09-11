package observation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequiredAuditFailureDegradesHealth(t *testing.T) {
	component := New()
	if err := component.WriteRequired(t.Context(), Runtime{}, "user", nil, 0, "test.audit", map[string]any{"ok": true}); err == nil {
		t.Fatal("required audit write without runtime succeeded")
	}
	health := component.HealthSnapshot(t.Context(), Runtime{})
	if health.Status != "degraded" || health.FailureCount != 1 {
		t.Fatalf("health after failed audit = %+v", health)
	}
}

func TestPrepareDiagnosticsDownloadOwnsSafeAttachmentMetadata(t *testing.T) {
	recorder := httptest.NewRecorder()
	version := New().PrepareDiagnosticsDownload(recorder)
	if version != ReportFormatVersion() {
		t.Fatalf("report format version = %q, want %q", version, ReportFormatVersion())
	}
	if disposition := recorder.Header().Get("Content-Disposition"); !strings.Contains(disposition, "aipermission-diagnostics-") || !strings.Contains(disposition, ".json") {
		t.Fatalf("content disposition = %q", disposition)
	}
	if recorder.Header().Get("Content-Type") != "application/json" || recorder.Header().Get("Cache-Control") != "no-store, private" {
		t.Fatalf("unsafe diagnostics headers = %v", recorder.Header())
	}
}

func TestHTTPHandlersShareActiveRuntimeBoundary(t *testing.T) {
	calls := 0
	handlers := New().HTTPHandlers(func(w http.ResponseWriter) (Runtime, bool) {
		calls++
		http.Error(w, "locked", http.StatusLocked)
		return Runtime{}, false
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
