package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/diagnostics"
)

func TestDiagnosticsDownloadRequiresUISessionAndReturnsAttachment(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()

	unauthorized := performJSONWithoutUICookie(handler, http.MethodGet, "/api/settings/diagnostics", "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("diagnostics without UI session = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	const secret = "ROUTE_DIAGNOSTICS_SECRET"
	if _, err := fixture.db.Exec(`INSERT INTO history_entries (
		source_ref_type, source_ref_id, connector_kind, activity_type, target_name, profile_label,
		status, input_text, output_text, error, created_at, updated_at
	) VALUES ('fixture', 999, 'ssh', 'action', 'private-target', 'private-profile',
		'failed', 'private-command', 'private-output', ?, datetime('now'), datetime('now'))`,
		"dial tcp 192.0.2.10:22: connection refused "+secret,
	); err != nil {
		t.Fatalf("insert route diagnostics fixture: %v", err)
	}

	response := performJSON(handler, http.MethodGet, "/api/settings/diagnostics", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("diagnostics response = %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store, private" {
		t.Fatalf("diagnostics cache control = %q", response.Header().Get("Cache-Control"))
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("diagnostics content type = %q", contentType)
	}
	if disposition := response.Header().Get("Content-Disposition"); !strings.Contains(disposition, "attachment;") || !strings.Contains(disposition, "aipermission-diagnostics-") {
		t.Fatalf("diagnostics content disposition = %q", disposition)
	}
	if strings.Contains(response.Body.String(), secret) || strings.Contains(response.Body.String(), "192.0.2.10") || strings.Contains(response.Body.String(), "private-target") {
		t.Fatalf("diagnostics route exposed private fixture data: %s", response.Body.String())
	}
	var report diagnostics.Report
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode diagnostics report: %v", err)
	}
	if report.ReportFormatVersion != diagnostics.ReportFormatVersion || report.Runtime.Gateway != "running" {
		t.Fatalf("unexpected diagnostics report: %+v", report)
	}
	var auditCount int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'settings.diagnostics.downloaded'`).Scan(&auditCount); err != nil {
		t.Fatalf("read diagnostics audit event: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("diagnostics audit events = %d, want 1", auditCount)
	}
}
