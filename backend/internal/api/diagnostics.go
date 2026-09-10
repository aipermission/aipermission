package api

import (
	"net/http"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/httpattachment"
	"github.com/aipermission/aipermission/backend/internal/observability"
)

func (h diagnosticsHandlers) download(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	audit := h.auditHealth.Snapshot(r.Context(), runtime.Storage.Database)
	report, err := observability.Collect(r.Context(), observability.CollectInput{
		Database:               runtime.Storage.Database,
		Registry:               runtimeConnectorRegistry(runtime),
		SupportedSchemaVersion: dbpkg.CurrentSchemaVersion(),
		MCPEnabled:             runtime.IsMCPStarted(),
		Audit: observability.AuditHealth{
			Status: audit.Status, FailureCount: audit.FailureCount, PendingCount: audit.PendingCount,
			DeadLetterCount:   audit.DeadLetterCount,
			RetriedEventCount: audit.RetriedEventCount,
		},
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	h.writeObservationAudit(r.Context(), runtime, "user", nil, 0, "settings.diagnostics.downloaded", map[string]any{
		"report_format_version": observability.ReportFormatVersion,
	})
	httpattachment.SetHeaders(w, "aipermission-diagnostics-"+time.Now().UTC().Format("20060102T150405Z")+".json", "application/json")
	writeJSON(w, http.StatusOK, report)
}
