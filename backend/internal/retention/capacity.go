package retention

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/retention/capacityreport"
)

func (h *HTTPHandlers) Capacity(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveService(w)
	if !ok {
		return
	}
	if !scope.Service.available() {
		httptransport.WriteInternalError(w)
		return
	}
	limits, err := actioncapacity.RuntimeLimits()
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	report, err := capacityreport.ReadReport(r.Context(), scope.Service.database, limits)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	settings, err := scope.Service.Read(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteSensitiveJSON(w, http.StatusOK, struct {
		capacityreport.Report
		HistoryDays int `json:"history_days"`
	}{Report: report, HistoryDays: settings.HistoryDays})
}
