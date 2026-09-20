package backups

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func (h *HTTPHandlers) BackupProviderStorage(w http.ResponseWriter, r *http.Request) {
	_, _, client, releaseProvider, ok := h.activeBackupServiceProvider(w, r, 0)
	if !ok {
		return
	}
	defer releaseProvider()
	usage, err := client.StorageUsage(r.Context())
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, usage)
}

func (h *HTTPHandlers) BackupProviderRetention(w http.ResponseWriter, r *http.Request) {
	_, provider, client, releaseProvider, ok := h.activeBackupServiceProvider(w, r, 0)
	if !ok {
		return
	}
	defer releaseProvider()
	policy, err := client.GetRetentionPolicy(r.Context(), stringFromMap(provider.Public, "stream_id"))
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, policy)
}

func (h *HTTPHandlers) PreviewBackupProviderRetention(w http.ResponseWriter, r *http.Request) {
	_, provider, client, releaseProvider, ok := h.activeBackupServiceProvider(w, r, 0)
	if !ok {
		return
	}
	defer releaseProvider()
	var request pruneBackupProviderRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	if request.KeepLatest < 1 || request.KeepLatest > 1000 {
		httptransport.WriteError(w, http.StatusBadRequest, "keep_latest must be between 1 and 1000")
		return
	}
	preview, err := client.PreviewRetention(r.Context(), stringFromMap(provider.Public, "stream_id"), request.KeepLatest)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, preview)
}

func (h *HTTPHandlers) UpdateBackupProviderRetention(w http.ResponseWriter, r *http.Request) {
	runtime, provider, client, releaseProvider, ok := h.activeBackupServiceProvider(w, r, requireRequiredAudit|requireObservation)
	if !ok {
		return
	}
	defer releaseProvider()
	var request updateBackupRetentionRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	if (request.Enabled && (request.KeepLatest < 1 || request.KeepLatest > 1000)) || (!request.Enabled && request.KeepLatest != 0) {
		httptransport.WriteError(w, http.StatusBadRequest, "enabled retention requires keep_latest between 1 and 1000; disabled retention requires 0")
		return
	}
	streamID := stringFromMap(provider.Public, "stream_id")
	if err := runtime.AuditRequired(r.Context(), "backup.provider.retention.update_requested", map[string]any{
		"provider_id": provider.ID, "provider_type": provider.ProviderType,
		"stream_id": streamID, "enabled": request.Enabled,
		"keep_latest": request.KeepLatest, "apply_now": request.ApplyNow,
	}); err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	result, err := client.UpdateRetention(
		r.Context(), streamID,
		request.Enabled, request.KeepLatest, request.ApplyNow,
	)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	runtime.Observe(r.Context(), "backup.provider.retention.updated", map[string]any{
		"provider_id": provider.ID, "provider_type": provider.ProviderType,
		"stream_id": result.Policy.StreamID, "enabled": result.Policy.Enabled,
		"keep_latest": result.Policy.KeepLatest, "apply_now": request.ApplyNow,
		"deleted_count": result.DeletedCount,
	})
	httptransport.WriteJSON(w, http.StatusOK, result)
}
