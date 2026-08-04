package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/backups"
)

func (s backupHandlers) listProviderRecords(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	store := backups.NewStore(runtime.database)
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
	records, err := store.ListRecords(r.Context(), backups.ListRecordsFilter{ProviderID: id})
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	responses := make([]backupRecordResponse, 0, len(records))
	for _, item := range records {
		responses = append(responses, backupRecordToResponse(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": responses, "remote_sync": provider.Status == "active", "freshness": syncResult.Freshness})
}

func (s backupHandlers) backupFreshness(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	store := backups.NewStore(runtime.database)
	providers, err := store.ListProviders(r.Context())
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	warnings := make([]backupFreshnessResponse, 0)
	checkErrors := make([]map[string]any, 0)
	for _, provider := range providers {
		if provider.Status != "active" || provider.ProviderType != backups.ServiceProviderType {
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
	writeJSON(w, http.StatusOK, map[string]any{"items": warnings, "check_errors": checkErrors})
}

func (s backupHandlers) uploadProviderBackup(w http.ResponseWriter, r *http.Request) {
	runtime, provider, client, ok := s.activeBackupServiceProvider(w, r)
	if !ok {
		return
	}
	s.lifecycleMu.RLock()
	snapshot, err := createDatabaseSnapshot(runtime)
	s.lifecycleMu.RUnlock()
	if err != nil {
		writeInternalError(w)
		return
	}
	defer os.Remove(snapshot.Path)
	streamID := stringFromMap(provider.Public, "stream_id")
	remoteName := stringFromMap(provider.Public, "database_name")
	backup, err := client.Upload(r.Context(), streamID, remoteName, backupSourceInstallationID(s.config.DataPath), snapshot.Path)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	record, err := upsertServiceBackupRecord(r.Context(), runtime, backups.NewStore(runtime.database), provider, backup)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	if err := backups.WriteServiceBaseline(r.Context(), runtime.database, stringFromMap(provider.Public, "base_url"), streamID, backup); err != nil {
		handleBackupProviderError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.uploaded", map[string]any{
		"provider_id": provider.ID, "provider_type": provider.ProviderType,
		"provider_file_id": backup.ID, "filename": backup.Filename, "size_bytes": backup.SizeBytes,
	})
	writeJSON(w, http.StatusCreated, backupRecordToResponse(record))
}

func (s backupHandlers) pruneProviderBackups(w http.ResponseWriter, r *http.Request) {
	runtime, provider, client, ok := s.activeBackupServiceProvider(w, r)
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
	result, err := client.PruneBackups(r.Context(), stringFromMap(provider.Public, "stream_id"), request.KeepLatest)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	store := backups.NewStore(runtime.database)
	if _, err := syncBackupServiceRecords(r.Context(), runtime, store, provider); err != nil {
		handleBackupServiceError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.pruned", map[string]any{
		"provider_id": provider.ID, "provider_type": provider.ProviderType,
		"stream_id": result.StreamID, "keep_latest": result.KeepLatest, "deleted_count": result.DeletedCount,
	})
	writeJSON(w, http.StatusOK, result)
}

func (s backupHandlers) deleteProviderBackupRecords(w http.ResponseWriter, r *http.Request) {
	runtime, provider, client, ok := s.activeBackupServiceProvider(w, r)
	if !ok {
		return
	}
	var request deleteBackupRecordsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if len(request.RecordIDs) < 1 || len(request.RecordIDs) > 100 {
		writeError(w, http.StatusBadRequest, "record_ids must contain 1 to 100 backup record ids")
		return
	}
	store := backups.NewStore(runtime.database)
	seen := make(map[int64]struct{}, len(request.RecordIDs))
	providerFileIDs := make([]string, 0, len(request.RecordIDs))
	streamID := stringFromMap(provider.Public, "stream_id")
	for _, recordID := range request.RecordIDs {
		if recordID < 1 {
			writeError(w, http.StatusBadRequest, "backup record ids must be positive")
			return
		}
		if _, exists := seen[recordID]; exists {
			writeError(w, http.StatusBadRequest, "backup record ids must be unique")
			return
		}
		seen[recordID] = struct{}{}
		record, err := store.GetRecord(r.Context(), provider.ID, recordID)
		if err != nil {
			handleBackupProviderError(w, err)
			return
		}
		if stringFromMap(record.Metadata, "stream_id") != streamID {
			writeError(w, http.StatusConflict, "backup record does not belong to the provider stream")
			return
		}
		providerFileIDs = append(providerFileIDs, record.ProviderFileID)
	}
	result, err := client.DeleteBackups(r.Context(), streamID, providerFileIDs)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	if err := store.MarkProviderRecordsDeleted(r.Context(), provider.ID, result.DeletedIDs); err != nil {
		handleBackupProviderError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.records.deleted", map[string]any{
		"provider_id": provider.ID, "provider_type": provider.ProviderType,
		"stream_id": result.StreamID, "record_ids": request.RecordIDs, "deleted_count": result.DeletedCount,
	})
	writeJSON(w, http.StatusOK, result)
}

func (s backupHandlers) downloadProviderRecord(w http.ResponseWriter, r *http.Request) {
	runtime, provider, record, client, ok := s.resolveBackupServiceRecord(w, r)
	if !ok {
		return
	}
	tmpPath, err := downloadServiceRecordToTemp(r.Context(), runtime, provider, record, client)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	defer os.Remove(tmpPath)
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.record.downloaded", map[string]any{
		"provider_id": provider.ID, "record_id": record.ID, "filename": record.Filename,
	})
	filename := safeBackupDownloadFilename(record.Filename, record.DatabaseName)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	http.ServeFile(w, r, tmpPath)
}

func (s backupHandlers) restoreProviderRecord(w http.ResponseWriter, r *http.Request) {
	runtime, provider, record, client, ok := s.resolveBackupServiceRecord(w, r)
	if !ok {
		return
	}
	var request restoreBackupRecordRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	defer clearStringReferences(&request.DatabasePassword)
	tmpPath, err := downloadServiceRecordToTemp(r.Context(), runtime, provider, record, client)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	defer os.Remove(tmpPath)
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.record.restore_requested", map[string]any{
		"provider_id": provider.ID, "record_id": record.ID, "filename": record.Filename,
		"database_name": strings.TrimSpace(request.DatabaseName), "source_machine": record.SourceMachine,
	})
	s.installImportedDatabaseWithMutator(w, r, request.DatabaseName, request.DatabasePassword, copyBackupFile(tmpPath), func(database *sql.DB) error {
		return backups.WriteServiceBaseline(r.Context(), database, stringFromMap(provider.Public, "base_url"), stringFromMap(provider.Public, "stream_id"), backups.ServiceBackup{
			ID: record.ProviderFileID, CreatedAt: record.BackupCreatedAt,
		})
	})
}
