package backups

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func (h *HTTPHandlers) activeBackupServiceProvider(w http.ResponseWriter, r *http.Request, requirements scopeRequirements) (HTTPScope, Provider, *ServiceClient, bool) {
	runtime, ok := h.resolve(w, requireDatabase|requireSecrets|requirements)
	if !ok {
		return HTTPScope{}, Provider{}, nil, false
	}
	provider, client, ok := activeBackupServiceProviderFromScope(w, r, runtime)
	return runtime, provider, client, ok
}

func activeBackupServiceProviderFromScope(w http.ResponseWriter, r *http.Request, runtime HTTPScope) (Provider, *ServiceClient, bool) {
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return Provider{}, nil, false
	}
	provider, err := NewStore(runtime.Database).GetProvider(r.Context(), id)
	if err != nil {
		handleBackupProviderError(w, err)
		return Provider{}, nil, false
	}
	if provider.Status != "active" {
		httptransport.WriteError(w, http.StatusConflict, "backup provider is disabled")
		return Provider{}, nil, false
	}
	client, err := backupServiceClient(runtime, provider)
	if err != nil {
		handleBackupProviderError(w, err)
		return Provider{}, nil, false
	}
	return provider, client, true
}

func resolveBackupServiceRecordFromScope(w http.ResponseWriter, r *http.Request, runtime HTTPScope, provider Provider) (Record, bool) {
	recordID, ok := httptransport.ParsePathInt64(w, r, "record_id", "backup record id is required")
	if !ok {
		return Record{}, false
	}
	record, err := NewStore(runtime.Database).GetRecord(r.Context(), provider.ID, recordID)
	if err != nil {
		handleBackupProviderError(w, err)
		return Record{}, false
	}
	return record, true
}

func normalizeServiceProviderPublic(runtime HTTPScope, submitted, existing map[string]any) (map[string]any, error) {
	baseURL := stringFromMap(submitted, "base_url")
	if baseURL == "" {
		baseURL = stringFromMap(existing, "base_url")
	}
	normalizedURL, err := ValidateServiceURL(baseURL)
	if err != nil {
		return nil, err
	}
	databaseName := stringFromMap(existing, "database_name")
	if databaseName == "" {
		databaseName = runtime.DatabaseName
	}
	return map[string]any{
		"base_url":         normalizedURL,
		"stream_id":        runtime.WorkspaceUUID,
		"database_name":    databaseName,
		"protocol_version": ServiceProtocol,
	}, nil
}

func backupServiceTokenSecret(secret map[string]any, required bool) (map[string]any, error) {
	token := stringFromMap(secret, "token")
	if token == "" {
		if required {
			return nil, ValidationError("backup service token is required")
		}
		return nil, nil
	}
	if err := ValidateServiceToken(token); err != nil {
		return nil, err
	}
	return map[string]any{"token": token}, nil
}

func decryptBackupProviderSecret(runtime HTTPScope, provider Provider) (map[string]any, error) {
	if provider.EncryptedSecretJSON == "" {
		return map[string]any{}, nil
	}
	secrets, err := runtime.Secrets.DecryptProviderSecret(provider)
	if err != nil {
		return nil, err
	}
	return secrets, nil
}

func backupServiceClient(runtime HTTPScope, provider Provider) (*ServiceClient, error) {
	if provider.ProviderType != ServiceProviderType {
		return nil, ValidationError("unsupported backup provider type")
	}
	secrets, err := decryptBackupProviderSecret(runtime, provider)
	if err != nil {
		return nil, fmt.Errorf("decrypt backup provider secret: %w", err)
	}
	return NewServiceClient(stringFromMap(provider.Public, "base_url"), stringFromMap(secrets, "token"))
}

func validateBackupProviderPayload(w http.ResponseWriter, public, secret map[string]any) bool {
	for label, value := range map[string]map[string]any{"public": public, "secret": secret} {
		if value == nil {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			httptransport.WriteError(w, http.StatusBadRequest, "invalid "+label+" json")
			return false
		}
		if len(encoded) > maxBackupProviderJSONBytes {
			httptransport.WriteError(w, http.StatusBadRequest, label+" json is too large")
			return false
		}
	}
	return true
}

func ProviderToResponse(item Provider) ProviderResponse {
	return ProviderResponse{
		ID: item.ID, ProviderType: item.ProviderType, Name: item.Name, Status: item.Status,
		Public: item.Public, HasSecret: item.EncryptedSecretJSON != "", LastCheckedAt: item.LastCheckedAt,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func RecordToResponse(item Record) RecordResponse {
	return RecordResponse{
		ID: item.ID, ProviderID: item.ProviderID, DatabaseID: item.DatabaseID, DatabaseName: item.DatabaseName,
		ProviderFileID: item.ProviderFileID, Filename: item.Filename, SourceMachine: item.SourceMachine,
		SizeBytes: item.SizeBytes, ChecksumSHA256: item.ChecksumSHA256, BackupCreatedAt: item.BackupCreatedAt,
		UploadedAt: item.UploadedAt, Metadata: item.Metadata, DeletedAt: item.DeletedAt,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func syncBackupServiceRecords(ctx context.Context, runtime HTTPScope, store *Store, provider Provider) (backupSyncResult, error) {
	baseURL := stringFromMap(provider.Public, "base_url")
	streamID := stringFromMap(provider.Public, "stream_id")
	baseline, err := ReadServiceBaseline(ctx, runtime.Database, baseURL, streamID)
	if err != nil {
		return backupSyncResult{}, err
	}
	client, err := backupServiceClient(runtime, provider)
	if err != nil {
		return backupSyncResult{}, err
	}
	items, err := client.ListBackups(ctx, streamID)
	if err != nil {
		return backupSyncResult{}, err
	}
	result := backupSyncResult{Freshness: backupFreshnessResponse{ProviderID: provider.ID, ProviderName: provider.Name}}
	if baseline != nil {
		result.Freshness.LatestKnownID = baseline.BackupID
		result.Freshness.LatestKnownAt = baseline.CreatedAt
	}
	if len(items) > 0 {
		result.Freshness.LatestRemoteID = items[0].ID
		result.Freshness.LatestRemoteAt = items[0].CreatedAt
		result.Freshness.LatestRemoteSource = items[0].SourceInstallationID
		result.Freshness.RemoteNewer = remoteBackupIsNewer(items[0], baseline)
	}
	presentIDs := make([]string, 0, len(items))
	for _, item := range items {
		if _, err := upsertServiceBackupRecord(ctx, runtime, store, provider, item); err != nil {
			return backupSyncResult{}, err
		}
		presentIDs = append(presentIDs, item.ID)
	}
	if err := store.MarkMissingProviderRecordsDeleted(ctx, provider.ID, presentIDs); err != nil {
		return backupSyncResult{}, err
	}
	if err := store.UpdateLastChecked(ctx, provider.ID, time.Now()); err != nil {
		return backupSyncResult{}, err
	}
	return result, nil
}

func remoteBackupIsNewer(remote ServiceBackup, baseline *ServiceBaseline) bool {
	if remote.ID == "" {
		return false
	}
	if baseline == nil {
		return true
	}
	if remote.ID == baseline.BackupID {
		return false
	}
	remoteAt, remoteErr := time.Parse(time.RFC3339Nano, remote.CreatedAt)
	knownAt, knownErr := time.Parse(time.RFC3339Nano, baseline.CreatedAt)
	if remoteErr != nil || knownErr != nil {
		return false
	}
	return !remoteAt.Before(knownAt)
}

func upsertServiceBackupRecord(ctx context.Context, runtime HTTPScope, store *Store, provider Provider, item ServiceBackup) (Record, error) {
	return store.UpsertRecord(ctx, CreateRecordRequest{
		ProviderID: provider.ID, DatabaseID: runtime.DatabaseID, DatabaseName: item.DatabaseName,
		ProviderFileID: item.ID, Filename: item.Filename, SourceMachine: item.SourceInstallationID,
		SizeBytes: item.SizeBytes, ChecksumSHA256: item.SHA256, BackupCreatedAt: item.CreatedAt, UploadedAt: item.CreatedAt,
		Metadata: map[string]any{"provider_type": provider.ProviderType, "stream_id": item.StreamID},
	})
}

func downloadServiceRecordToTemp(ctx context.Context, runtime HTTPScope, provider Provider, record Record, client *ServiceClient) (string, error) {
	if record.SizeBytes < 1 || record.SizeBytes > MaxDatabaseTransferBytes {
		return "", ValidationError("backup is too large to download through the gateway")
	}
	tmpPath, err := databasecatalog.ReserveTempPath(runtime.DatabasePath, fmt.Sprintf("remote-backup-%d-*.aipdb", provider.ID))
	if err != nil {
		return "", err
	}
	downloaded, err := client.Download(ctx, stringFromMap(provider.Public, "stream_id"), record.ProviderFileID, tmpPath, MaxDatabaseTransferBytes)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", remoteOperationError{err: err}
	}
	if downloaded.SizeBytes != record.SizeBytes || !strings.EqualFold(downloaded.SHA256, record.ChecksumSHA256) {
		_ = os.Remove(tmpPath)
		return "", remoteOperationError{err: errors.New("remote backup metadata changed since it was listed; refresh versions and try again")}
	}
	return tmpPath, nil
}

func CopyBackupFile(sourcePath string) func(string) error {
	return func(targetPath string) error {
		source, err := os.Open(sourcePath)
		if err != nil {
			return err
		}
		defer source.Close()
		target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		remove := true
		defer func() {
			_ = target.Close()
			if remove {
				_ = os.Remove(targetPath)
			}
		}()
		if _, err := io.Copy(target, source); err != nil {
			return err
		}
		if err := target.Sync(); err != nil {
			return err
		}
		if err := target.Close(); err != nil {
			return err
		}
		remove = false
		return nil
	}
}

func backupSourceInstallationID(dataPath string) string {
	hostname, _ := os.Hostname()
	digest := sha256.Sum256([]byte(hostname + "\x00" + filepath.Clean(dataPath)))
	return "install_" + hex.EncodeToString(digest[:12])
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func safeBackupDownloadFilename(filename, databaseName string) string {
	filename = strings.TrimSpace(filename)
	filename = strings.ReplaceAll(filename, "\\", "/")
	filename = filepath.Base(filename)
	if filename == "" || filename == "." || filename == "/" {
		filename = strings.TrimSpace(databaseName) + ".aipdb"
	}
	return strings.ReplaceAll(filename, `"`, "")
}

func cloneJSONMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func handleBackupProviderError(w http.ResponseWriter, err error) {
	var remoteErr remoteOperationError
	if errors.As(err, &remoteErr) {
		handleBackupServiceError(w, remoteErr.err)
		return
	}
	switch {
	case errors.Is(err, ErrProviderDisabled):
		httptransport.WriteError(w, http.StatusConflict, ErrProviderDisabled.Error())
	case errors.Is(err, ErrNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "backup provider not found")
	case errors.Is(err, ErrRecordNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "backup record not found")
	default:
		var validation ValidationError
		if errors.As(err, &validation) {
			httptransport.WriteError(w, http.StatusBadRequest, validation.Error())
			return
		}
		httptransport.WriteInternalError(w)
	}
}

func WriteProviderHTTPError(w http.ResponseWriter, err error) {
	handleBackupProviderError(w, err)
}

func handleBackupServiceError(w http.ResponseWriter, err error) {
	var validation ValidationError
	if errors.As(err, &validation) {
		httptransport.WriteError(w, http.StatusBadRequest, validation.Error())
		return
	}
	var serviceError ServiceError
	if errors.As(err, &serviceError) {
		switch serviceError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			httptransport.WriteError(w, http.StatusBadGateway, "backup service rejected its access token")
		case http.StatusNotFound:
			if serviceError.Code == "backup_not_found" || serviceError.Code == "stream_not_found" {
				httptransport.WriteError(w, http.StatusNotFound, "backup service stream or version was not found")
			} else {
				httptransport.WriteError(w, http.StatusConflict, "backup service does not support this operation; upgrade AIPermission Backup")
			}
		case http.StatusRequestEntityTooLarge:
			httptransport.WriteError(w, http.StatusRequestEntityTooLarge, "backup exceeds the remote service upload limit")
		case http.StatusInsufficientStorage:
			httptransport.WriteError(w, http.StatusInsufficientStorage, "backup service storage quota is full")
		case http.StatusUpgradeRequired:
			httptransport.WriteError(w, http.StatusConflict, "backup service protocol is incompatible with this AIPermission version")
		default:
			httptransport.WriteError(w, http.StatusBadGateway, "backup service request failed")
		}
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		httptransport.WriteError(w, http.StatusGatewayTimeout, "backup service request timed out")
		return
	}
	httptransport.WriteError(w, http.StatusBadGateway, "backup service request failed")
}

func WriteServiceHTTPError(w http.ResponseWriter, err error) {
	handleBackupServiceError(w, err)
}
