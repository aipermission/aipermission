package api

import "net/http"

func (s backupHandlers) backupProviderStorage(w http.ResponseWriter, r *http.Request) {
	_, _, client, ok := s.activeBackupServiceProvider(w, r)
	if !ok {
		return
	}
	usage, err := client.StorageUsage(r.Context())
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func (s backupHandlers) backupProviderRetention(w http.ResponseWriter, r *http.Request) {
	_, provider, client, ok := s.activeBackupServiceProvider(w, r)
	if !ok {
		return
	}
	policy, err := client.GetRetentionPolicy(r.Context(), stringFromMap(provider.Public, "stream_id"))
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (s backupHandlers) previewBackupProviderRetention(w http.ResponseWriter, r *http.Request) {
	_, provider, client, ok := s.activeBackupServiceProvider(w, r)
	if !ok {
		return
	}
	var request pruneBackupProviderRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if request.KeepLatest < 1 || request.KeepLatest > 1000 {
		writeError(w, http.StatusBadRequest, "keep_latest must be between 1 and 1000")
		return
	}
	preview, err := client.PreviewRetention(r.Context(), stringFromMap(provider.Public, "stream_id"), request.KeepLatest)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s backupHandlers) updateBackupProviderRetention(w http.ResponseWriter, r *http.Request) {
	runtime, provider, client, ok := s.activeBackupServiceProvider(w, r)
	if !ok {
		return
	}
	var request updateBackupRetentionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if (request.Enabled && (request.KeepLatest < 1 || request.KeepLatest > 1000)) || (!request.Enabled && request.KeepLatest != 0) {
		writeError(w, http.StatusBadRequest, "enabled retention requires keep_latest between 1 and 1000; disabled retention requires 0")
		return
	}
	result, err := client.UpdateRetention(
		r.Context(), stringFromMap(provider.Public, "stream_id"),
		request.Enabled, request.KeepLatest, request.ApplyNow,
	)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.retention.updated", map[string]any{
		"provider_id": provider.ID, "provider_type": provider.ProviderType,
		"stream_id": result.Policy.StreamID, "enabled": result.Policy.Enabled,
		"keep_latest": result.Policy.KeepLatest, "apply_now": request.ApplyNow,
		"deleted_count": result.DeletedCount,
	})
	writeJSON(w, http.StatusOK, result)
}
