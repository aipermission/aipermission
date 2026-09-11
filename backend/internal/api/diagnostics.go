package api

import (
	"net/http"
	"time"

	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (h diagnosticsHandlers) download(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	report, err := h.observation.Diagnostics(r.Context(), observationRuntime(runtime))
	if err != nil {
		writeInternalError(w)
		return
	}
	h.writeObservationAudit(r.Context(), runtime, "user", nil, 0, "settings.diagnostics.downloaded", map[string]any{
		"report_format_version": gatewayoperations.ObservationReportFormatVersion(),
	})
	gatewayoperations.SetAttachmentHeaders(w, "aipermission-diagnostics-"+time.Now().UTC().Format("20060102T150405Z")+".json", "application/json")
	writeJSON(w, http.StatusOK, report)
}
