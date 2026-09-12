package api

import "net/http"

func (h diagnosticsHandlers) download(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	report, err := h.observationOwner.ObservationDiagnostics(r.Context(), runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	formatVersion := h.observationOwner.PrepareDiagnosticsDownload(w)
	h.writeObservationAudit(r.Context(), runtime, "user", nil, 0, "settings.diagnostics.downloaded", map[string]any{
		"report_format_version": formatVersion,
	})
	writeJSON(w, http.StatusOK, report)
}
