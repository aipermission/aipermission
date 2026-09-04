package api

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

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

func (s backupHandlers) activeBackupServiceProvider(w http.ResponseWriter, r *http.Request) (*databaseRuntime, backups.Provider, *backups.ServiceClient, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, backups.Provider{}, nil, false
	}
	id, ok := parseID(w, r)
	if !ok {
		return nil, backups.Provider{}, nil, false
	}
	provider, err := backups.NewStore(runtime.database).GetProvider(r.Context(), id)
	if err != nil {
		handleBackupProviderError(w, err)
		return nil, backups.Provider{}, nil, false
	}
	if provider.Status != "active" {
		writeError(w, http.StatusConflict, "backup provider is disabled")
		return nil, backups.Provider{}, nil, false
	}
	client, err := backupServiceClient(runtime, provider)
	if err != nil {
		handleBackupProviderError(w, err)
		return nil, backups.Provider{}, nil, false
	}
	return runtime, provider, client, true
}

func (s backupHandlers) resolveBackupServiceRecord(w http.ResponseWriter, r *http.Request) (*databaseRuntime, backups.Provider, backups.Record, *backups.ServiceClient, bool) {
	runtime, provider, client, ok := s.activeBackupServiceProvider(w, r)
	if !ok {
		return nil, backups.Provider{}, backups.Record{}, nil, false
	}
	recordID, ok := parsePathInt64(w, r, "record_id", "backup record id")
	if !ok {
		return nil, backups.Provider{}, backups.Record{}, nil, false
	}
	record, err := backups.NewStore(runtime.database).GetRecord(r.Context(), provider.ID, recordID)
	if err != nil {
		handleBackupProviderError(w, err)
		return nil, backups.Provider{}, backups.Record{}, nil, false
	}
	return runtime, provider, record, client, true
}

func (s backupHandlers) normalizeServiceProviderPublic(runtime *databaseRuntime, submitted, existing map[string]any) (map[string]any, error) {
	baseURL := stringFromMap(submitted, "base_url")
	if baseURL == "" {
		baseURL = stringFromMap(existing, "base_url")
	}
	normalizedURL, err := backups.ValidateServiceURL(baseURL)
	if err != nil {
		return nil, err
	}
	databaseName := stringFromMap(existing, "database_name")
	if databaseName == "" {
		databaseName = s.activeDatabaseDisplayName()
	}
	return map[string]any{
		"base_url":         normalizedURL,
		"stream_id":        runtime.workspaceUUID,
		"database_name":    databaseName,
		"protocol_version": backups.ServiceProtocol,
	}, nil
}

func (s backupHandlers) activeDatabaseDisplayName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentDatabaseNameLocked()
}

func backupServiceTokenSecret(secret map[string]any, required bool) (map[string]any, error) {
	token := stringFromMap(secret, "token")
	if token == "" {
		if required {
			return nil, backups.ValidationError("backup service token is required")
		}
		return nil, nil
	}
	if err := backups.ValidateServiceToken(token); err != nil {
		return nil, err
	}
	return map[string]any{"token": token}, nil
}

func decryptBackupProviderSecret(runtime *databaseRuntime, provider backups.Provider) (map[string]any, error) {
	if provider.EncryptedSecretJSON == "" {
		return map[string]any{}, nil
	}
	secrets := map[string]any{}
	if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.BackupProvider, provider.ID, provider.EncryptedSecretJSON, &secrets); err != nil {
		return nil, err
	}
	return secrets, nil
}

func backupServiceClient(runtime *databaseRuntime, provider backups.Provider) (*backups.ServiceClient, error) {
	if provider.ProviderType != backups.ServiceProviderType {
		return nil, backups.ValidationError("unsupported backup provider type")
	}
	secrets, err := decryptBackupProviderSecret(runtime, provider)
	if err != nil {
		return nil, fmt.Errorf("decrypt backup provider secret: %w", err)
	}
	return backups.NewServiceClient(stringFromMap(provider.Public, "base_url"), stringFromMap(secrets, "token"))
}

func validateBackupProviderPayload(w http.ResponseWriter, public, secret map[string]any) bool {
	for label, value := range map[string]map[string]any{"public": public, "secret": secret} {
		if value == nil {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid "+label+" json")
			return false
		}
		if len(encoded) > maxBackupProviderJSONBytes {
			writeError(w, http.StatusBadRequest, label+" json is too large")
			return false
		}
	}
	return true
}

func backupProviderToResponse(item backups.Provider) backupProviderResponse {
	return backupProviderResponse{
		ID: item.ID, ProviderType: item.ProviderType, Name: item.Name, Status: item.Status,
		Public: item.Public, HasSecret: item.EncryptedSecretJSON != "", LastCheckedAt: item.LastCheckedAt,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func backupRecordToResponse(item backups.Record) backupRecordResponse {
	return backupRecordResponse{
		ID: item.ID, ProviderID: item.ProviderID, DatabaseID: item.DatabaseID, DatabaseName: item.DatabaseName,
		ProviderFileID: item.ProviderFileID, Filename: item.Filename, SourceMachine: item.SourceMachine,
		SizeBytes: item.SizeBytes, ChecksumSHA256: item.ChecksumSHA256, BackupCreatedAt: item.BackupCreatedAt,
		UploadedAt: item.UploadedAt, Metadata: item.Metadata, DeletedAt: item.DeletedAt,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func syncBackupServiceRecords(ctx context.Context, runtime *databaseRuntime, store *backups.Store, provider backups.Provider) (backupSyncResult, error) {
	baseURL := stringFromMap(provider.Public, "base_url")
	streamID := stringFromMap(provider.Public, "stream_id")
	baseline, err := backups.ReadServiceBaseline(ctx, runtime.database, baseURL, streamID)
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

func remoteBackupIsNewer(remote backups.ServiceBackup, baseline *backups.ServiceBaseline) bool {
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

func upsertServiceBackupRecord(ctx context.Context, runtime *databaseRuntime, store *backups.Store, provider backups.Provider, item backups.ServiceBackup) (backups.Record, error) {
	return store.UpsertRecord(ctx, backups.CreateRecordRequest{
		ProviderID: provider.ID, DatabaseID: runtime.id, DatabaseName: item.DatabaseName,
		ProviderFileID: item.ID, Filename: item.Filename, SourceMachine: item.SourceInstallationID,
		SizeBytes: item.SizeBytes, ChecksumSHA256: item.SHA256, BackupCreatedAt: item.CreatedAt, UploadedAt: item.CreatedAt,
		Metadata: map[string]any{"provider_type": provider.ProviderType, "stream_id": item.StreamID},
	})
}

func downloadServiceRecordToTemp(ctx context.Context, runtime *databaseRuntime, provider backups.Provider, record backups.Record, client *backups.ServiceClient) (string, error) {
	if record.SizeBytes < 1 || record.SizeBytes > maxImportBodyBytes {
		return "", backups.ValidationError("backup is too large to download through the gateway")
	}
	tmpPath, err := reserveDatabaseTempPath(runtime.path, fmt.Sprintf("remote-backup-%d-*.aipdb", provider.ID))
	if err != nil {
		return "", err
	}
	downloaded, err := client.Download(ctx, stringFromMap(provider.Public, "stream_id"), record.ProviderFileID, tmpPath, maxImportBodyBytes)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if downloaded.SizeBytes != record.SizeBytes || !strings.EqualFold(downloaded.SHA256, record.ChecksumSHA256) {
		_ = os.Remove(tmpPath)
		return "", errors.New("remote backup metadata changed since it was listed; refresh versions and try again")
	}
	return tmpPath, nil
}

func copyBackupFile(sourcePath string) func(string) error {
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
	switch {
	case errors.Is(err, backups.ErrNotFound):
		writeError(w, http.StatusNotFound, "backup provider not found")
	case errors.Is(err, backups.ErrRecordNotFound):
		writeError(w, http.StatusNotFound, "backup record not found")
	default:
		var validation backups.ValidationError
		if errors.As(err, &validation) {
			writeError(w, http.StatusBadRequest, validation.Error())
			return
		}
		writeInternalError(w)
	}
}

func handleBackupServiceError(w http.ResponseWriter, err error) {
	var validation backups.ValidationError
	if errors.As(err, &validation) {
		writeError(w, http.StatusBadRequest, validation.Error())
		return
	}
	var serviceError backups.ServiceError
	if errors.As(err, &serviceError) {
		switch serviceError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			writeError(w, http.StatusBadGateway, "backup service rejected its access token")
		case http.StatusNotFound:
			if serviceError.Code == "backup_not_found" || serviceError.Code == "stream_not_found" {
				writeError(w, http.StatusNotFound, "backup service stream or version was not found")
			} else {
				writeError(w, http.StatusConflict, "backup service does not support this operation; upgrade AIPermission Backup")
			}
		case http.StatusRequestEntityTooLarge:
			writeError(w, http.StatusRequestEntityTooLarge, "backup exceeds the remote service upload limit")
		case http.StatusInsufficientStorage:
			writeError(w, http.StatusInsufficientStorage, "backup service storage quota is full")
		case http.StatusUpgradeRequired:
			writeError(w, http.StatusConflict, "backup service protocol is incompatible with this AIPermission version")
		default:
			writeError(w, http.StatusBadGateway, "backup service request failed")
		}
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "backup service request timed out")
		return
	}
	writeError(w, http.StatusBadGateway, "backup service request failed")
}
