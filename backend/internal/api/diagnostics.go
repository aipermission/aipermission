package api

import (
	"net/http"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/diagnostics"
	"github.com/aipermission/aipermission/backend/internal/httpattachment"
)

func (h diagnosticsHandlers) download(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	audit := h.auditHealthSnapshot(r.Context())
	report, err := diagnostics.Collect(r.Context(), diagnostics.CollectInput{
		Database:               runtime.database,
		Registry:               runtime.connectorRegistry(),
		SupportedSchemaVersion: dbpkg.CurrentSchemaVersion(),
		MCPEnabled:             runtime.isMCPStarted(),
		Audit: diagnostics.AuditHealth{
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
		"report_format_version": diagnostics.ReportFormatVersion,
	})
	httpattachment.SetHeaders(w, "aipermission-diagnostics-"+time.Now().UTC().Format("20060102T150405Z")+".json", "application/json")
	writeJSON(w, http.StatusOK, report)
}
