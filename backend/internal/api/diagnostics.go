package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/buildinfo"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/diagnostics"
)

func (h diagnosticsHandlers) download(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	audit := h.auditHealthSnapshot(r.Context())
	report, err := diagnostics.Collect(r.Context(), diagnostics.CollectInput{
		Database:               runtime.database,
		Registry:               runtime.connectorRegistry(),
		ApplicationVersion:     buildinfo.Version,
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
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="aipermission-diagnostics-%s.json"`, time.Now().UTC().Format("20060102T150405Z")))
	writeJSON(w, http.StatusOK, report)
}
