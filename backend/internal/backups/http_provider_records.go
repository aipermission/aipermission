package backups

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/aipermission/aipermission/backend/internal/httpattachment"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func (h *HTTPHandlers) ListProviderRecords(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w, requireDatabase|requireDatabaseID|requireSecrets)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	store := NewStore(runtime.Database)
	provider, err := store.GetProvider(r.Context(), id)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	var syncResult backupSyncResult
	if provider.Status == "active" {
		if syncResult, err = syncBackupServiceRecords(r.Context(), runtime, store, provider); err != nil {
			handleBackupServiceError(w, err)
			return
		}
	}
	records, err := store.ListRecords(r.Context(), ListRecordsFilter{ProviderID: id})
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	responses := make([]RecordResponse, 0, len(records))
	for _, item := range records {
		responses = append(responses, RecordToResponse(item))
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": responses, "remote_sync": provider.Status == "active", "freshness": syncResult.Freshness})
}

func (h *HTTPHandlers) BackupFreshness(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w, requireDatabase|requireDatabaseID|requireSecrets)
	if !ok {
		return
	}
	store := NewStore(runtime.Database)
	providers, err := store.ListProviders(r.Context())
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	warnings := make([]backupFreshnessResponse, 0)
	checkErrors := make([]map[string]any, 0)
	for _, provider := range providers {
		if provider.Status != "active" || provider.ProviderType != ServiceProviderType {
			continue
		}
		result, syncErr := syncBackupServiceRecords(r.Context(), runtime, store, provider)
		if syncErr != nil {
			checkErrors = append(checkErrors, map[string]any{"provider_id": provider.ID, "provider_name": provider.Name})
			continue
		}
		if result.Freshness.RemoteNewer {
			warnings = append(warnings, result.Freshness)
		}
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": warnings, "check_errors": checkErrors})
}

func (h *HTTPHandlers) UploadProviderBackup(w http.ResponseWriter, r *http.Request) {
	const requirements = requireDatabase | requireDatabaseID | requireInstallationPath | requireSecrets | requireMutation | requireRequiredAudit | requireSnapshot
	runtime, releaseBackup, ok := h.resolveOperation(w, r, requirements)
	if !ok {
		return
	}
	defer releaseBackup()
	provider, client, ok := activeBackupServiceProviderFromScope(w, r, runtime)
	if !ok {
		return
	}
	var request struct {
		IdempotencyKey string `json:"idempotency_key"`
	}
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	streamID := stringFromMap(provider.Public, "stream_id")
	remoteName := stringFromMap(provider.Public, "database_name")
	sourceInstallationID := backupSourceInstallationID(runtime.InstallationDataPath)
	store := NewStore(runtime.Database)
	operation, _, err := store.ClaimUploadOperation(r.Context(), ClaimUploadOperationRequest{
		IdempotencyKey: request.IdempotencyKey, ProviderID: provider.ID, DatabaseID: runtime.DatabaseID,
		StreamID: streamID, SourceInstallationID: sourceInstallationID,
	})
	if errors.Is(err, ErrUploadIdempotencyConflict) {
		httptransport.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	if operation.Status == "completed" {
		record, recordErr := store.GetRecordByProviderFileID(r.Context(), provider.ID, operation.ProviderFileID)
		if errors.Is(recordErr, ErrUploadResultExpired) {
			httptransport.WriteError(w, http.StatusGone, recordErr.Error())
			return
		}
		if recordErr != nil {
			handleBackupProviderError(w, recordErr)
			return
		}
		httptransport.WriteJSON(w, http.StatusOK, RecordToResponse(record))
		return
	}
	snapshot, err := runtime.CreateSnapshot(r.Context())
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	defer os.Remove(snapshot.Path)
	if err := runtime.AuditRequired(r.Context(), "backup.provider.upload_requested", map[string]any{
		"provider_id": provider.ID, "provider_type": provider.ProviderType,
		"stream_id": streamID, "database_name": remoteName, "size_bytes": fileSize(snapshot.Path),
	}); err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	if err := store.MarkUploadDispatched(r.Context(), request.IdempotencyKey); err != nil {
		handleBackupProviderError(w, err)
		return
	}
	backup, _, err := client.Upload(r.Context(), streamID, remoteName, sourceInstallationID, request.IdempotencyKey, snapshot.Path)
	if err != nil {
		markBackupUploadOutcomeUnknown(runtime.Database, request.IdempotencyKey, err)
		handleBackupServiceError(w, err)
		return
	}
	var record Record
	err = runtime.Mutate(r.Context(), "backup.provider.uploaded", func() any {
		return map[string]any{
			"provider_id": provider.ID, "provider_type": provider.ProviderType,
			"provider_file_id": backup.ID, "filename": backup.Filename, "size_bytes": backup.SizeBytes,
			"retention_deleted_count": backup.RetentionDeletedCount,
		}
	}, func(tx *sql.Tx) error {
		txStore := NewTxStore(tx)
		var mutationErr error
		record, mutationErr = upsertServiceBackupRecord(r.Context(), runtime, txStore, provider, backup)
		if mutationErr != nil {
			return mutationErr
		}
		if mutationErr = WriteServiceBaseline(r.Context(), tx, stringFromMap(provider.Public, "base_url"), streamID, backup); mutationErr != nil {
			return mutationErr
		}
		return txStore.CompleteUploadOperation(r.Context(), request.IdempotencyKey, backup.ID)
	})
	if err != nil {
		markBackupUploadOutcomeUnknown(runtime.Database, request.IdempotencyKey, err)
		handleBackupProviderError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusCreated, RecordToResponse(record))
}

func markBackupUploadOutcomeUnknown(database *sql.DB, key string, operationErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = NewStore(database).MarkUploadOutcomeUnknown(ctx, key, operationErr)
}

func (h *HTTPHandlers) PruneProviderBackups(w http.ResponseWriter, r *http.Request) {
	runtime, provider, client, ok := h.activeBackupServiceProvider(w, r, requireDatabaseID|requireRequiredAudit|requireObservation)
	if !ok {
		return
	}
	var request pruneBackupProviderRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	if request.KeepLatest < 1 || request.KeepLatest > 1000 {
		httptransport.WriteError(w, http.StatusBadRequest, "keep_latest must be between 1 and 1000")
		return
	}
	streamID := stringFromMap(provider.Public, "stream_id")
	if err := runtime.AuditRequired(r.Context(), "backup.provider.prune_requested", map[string]any{
		"provider_id": provider.ID, "provider_type": provider.ProviderType,
		"stream_id": streamID, "keep_latest": request.KeepLatest,
	}); err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	result, err := client.PruneBackups(r.Context(), streamID, request.KeepLatest)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	store := NewStore(runtime.Database)
	if _, err := syncBackupServiceRecords(r.Context(), runtime, store, provider); err != nil {
		handleBackupServiceError(w, err)
		return
	}
	runtime.Observe(r.Context(), "backup.provider.pruned", map[string]any{
		"provider_id": provider.ID, "provider_type": provider.ProviderType,
		"stream_id": result.StreamID, "keep_latest": result.KeepLatest, "deleted_count": result.DeletedCount,
	})
	httptransport.WriteJSON(w, http.StatusOK, result)
}

func (h *HTTPHandlers) DeleteProviderBackupRecords(w http.ResponseWriter, r *http.Request) {
	runtime, provider, client, ok := h.activeBackupServiceProvider(w, r, requireMutation|requireRequiredAudit)
	if !ok {
		return
	}
	var request deleteBackupRecordsRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	if len(request.RecordIDs) < 1 || len(request.RecordIDs) > 100 {
		httptransport.WriteError(w, http.StatusBadRequest, "record_ids must contain 1 to 100 backup record ids")
		return
	}
	store := NewStore(runtime.Database)
	seen := make(map[int64]struct{}, len(request.RecordIDs))
	providerFileIDs := make([]string, 0, len(request.RecordIDs))
	streamID := stringFromMap(provider.Public, "stream_id")
	for _, recordID := range request.RecordIDs {
		if recordID < 1 {
			httptransport.WriteError(w, http.StatusBadRequest, "backup record ids must be positive")
			return
		}
		if _, exists := seen[recordID]; exists {
			httptransport.WriteError(w, http.StatusBadRequest, "backup record ids must be unique")
			return
		}
		seen[recordID] = struct{}{}
		record, err := store.GetRecord(r.Context(), provider.ID, recordID)
		if err != nil {
			handleBackupProviderError(w, err)
			return
		}
		if stringFromMap(record.Metadata, "stream_id") != streamID {
			httptransport.WriteError(w, http.StatusConflict, "backup record does not belong to the provider stream")
			return
		}
		providerFileIDs = append(providerFileIDs, record.ProviderFileID)
	}
	if err := runtime.AuditRequired(r.Context(), "backup.provider.records.delete_requested", map[string]any{
		"provider_id": provider.ID, "provider_type": provider.ProviderType,
		"stream_id": streamID, "record_ids": request.RecordIDs, "record_count": len(request.RecordIDs),
	}); err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	result, err := client.DeleteBackups(r.Context(), streamID, providerFileIDs)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	err = runtime.Mutate(r.Context(), "backup.provider.records.deleted", func() any {
		return map[string]any{
			"provider_id": provider.ID, "provider_type": provider.ProviderType,
			"stream_id": result.StreamID, "record_ids": request.RecordIDs, "deleted_count": result.DeletedCount,
		}
	}, func(tx *sql.Tx) error {
		return NewTxStore(tx).MarkProviderRecordsDeleted(r.Context(), provider.ID, result.DeletedIDs)
	})
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, result)
}

func (h *HTTPHandlers) DownloadProviderRecord(w http.ResponseWriter, r *http.Request) {
	const requirements = requireDatabase | requireDatabasePath | requireSecrets | requireObservation
	runtime, releaseBackup, ok := h.resolveOperation(w, r, requirements)
	if !ok {
		return
	}
	defer releaseBackup()
	provider, client, ok := activeBackupServiceProviderFromScope(w, r, runtime)
	if !ok {
		return
	}
	record, ok := resolveBackupServiceRecordFromScope(w, r, runtime, provider)
	if !ok {
		return
	}
	tmpPath, err := downloadServiceRecordToTemp(r.Context(), runtime, provider, record, client)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	defer os.Remove(tmpPath)
	runtime.Observe(r.Context(), "backup.provider.record.downloaded", map[string]any{
		"provider_id": provider.ID, "record_id": record.ID, "filename": record.Filename,
	})
	filename := safeBackupDownloadFilename(record.Filename, record.DatabaseName)
	httpattachment.SetHeaders(w, filename, "application/octet-stream")
	http.ServeFile(w, r, tmpPath)
}
