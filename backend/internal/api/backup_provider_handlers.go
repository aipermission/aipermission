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
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

const maxBackupProviderJSONBytes = 16 << 10

type backupProviderRequest struct {
	ProviderType string         `json:"provider_type"`
	Name         string         `json:"name"`
	Status       string         `json:"status,omitempty"`
	Public       map[string]any `json:"public,omitempty"`
	Secret       map[string]any `json:"secret,omitempty"`
}

type enableBackupProviderRequest struct {
	CurrentPassword string `json:"current_password"`
}

type restoreBackupRecordRequest struct {
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

type backupProviderCatalogItem struct {
	ProviderType string   `json:"provider_type"`
	Label        string   `json:"label"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
}

type backupProviderResponse struct {
	ID            int64          `json:"id"`
	ProviderType  string         `json:"provider_type"`
	Name          string         `json:"name"`
	Status        string         `json:"status"`
	Public        map[string]any `json:"public,omitempty"`
	HasSecret     bool           `json:"has_secret"`
	LastCheckedAt *string        `json:"last_checked_at,omitempty"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
}

type backupRecordResponse struct {
	ID              int64          `json:"id"`
	ProviderID      int64          `json:"provider_id"`
	DatabaseID      string         `json:"database_id"`
	DatabaseName    string         `json:"database_name"`
	ProviderFileID  string         `json:"provider_file_id"`
	Filename        string         `json:"filename"`
	SourceMachine   string         `json:"source_machine,omitempty"`
	SizeBytes       int64          `json:"size_bytes"`
	ChecksumSHA256  string         `json:"checksum_sha256,omitempty"`
	BackupCreatedAt string         `json:"backup_created_at"`
	UploadedAt      string         `json:"uploaded_at"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	DeletedAt       *string        `json:"deleted_at,omitempty"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
}

func (s backupHandlers) providerCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items": []backupProviderCatalogItem{{
			ProviderType: backups.ServiceProviderType,
			Label:        "AIPermission Backup",
			Status:       "available",
			Capabilities: []string{"encrypted_database_upload", "immutable_versions", "first_run_restore", "self_hosted"},
		}},
	})
}

func (s backupHandlers) listProviders(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	items, err := backups.NewStore(runtime.database).ListProviders(r.Context())
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	responses := make([]backupProviderResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, backupProviderToResponse(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": responses})
}

func (s backupHandlers) createProvider(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request backupProviderRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if !validateBackupProviderPayload(w, request.Public, request.Secret) {
		return
	}
	if strings.TrimSpace(request.ProviderType) != backups.ServiceProviderType {
		writeError(w, http.StatusBadRequest, "unsupported backup provider type")
		return
	}
	public, err := s.normalizeServiceProviderPublic(runtime, request.Public, nil)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	encrypted, err := encryptBackupServiceToken(runtime, request.Secret, true)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	item, err := backups.NewStore(runtime.database).CreateProvider(r.Context(), backups.CreateProviderRequest{
		ProviderType: backups.ServiceProviderType,
		Name:         strings.TrimSpace(request.Name),
		Status:       "disabled",
		Public:       public,
		Encrypted:    encrypted,
	})
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.created", map[string]any{
		"provider_id": item.ID, "provider_type": item.ProviderType, "name": item.Name, "status": item.Status,
	})
	writeJSON(w, http.StatusCreated, backupProviderToResponse(item))
}

func (s backupHandlers) updateProvider(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request backupProviderRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if !validateBackupProviderPayload(w, request.Public, request.Secret) {
		return
	}
	store := backups.NewStore(runtime.database)
	existing, err := store.GetProvider(r.Context(), id)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	public, err := s.normalizeServiceProviderPublic(runtime, request.Public, existing.Public)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	status := strings.TrimSpace(request.Status)
	if status == "" {
		status = existing.Status
	}
	if existing.Status != "active" && status == "active" {
		writeError(w, http.StatusConflict, "enable this provider with the explicit enable action")
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = existing.Name
	}
	var encrypted *string
	if request.Secret != nil {
		value, err := encryptBackupServiceToken(runtime, request.Secret, true)
		if err != nil {
			handleBackupProviderError(w, err)
			return
		}
		encrypted = &value
	}
	prospective := existing
	prospective.Public = public
	if encrypted != nil {
		prospective.EncryptedSecretJSON = *encrypted
	}
	if status == "active" {
		client, err := backupServiceClient(runtime, prospective)
		if err != nil {
			handleBackupProviderError(w, err)
			return
		}
		if _, err := client.Info(r.Context()); err != nil {
			handleBackupServiceError(w, err)
			return
		}
	}
	item, err := store.UpdateProvider(r.Context(), id, backups.UpdateProviderRequest{
		Name: name, Status: status, Public: public, Encrypted: encrypted,
	})
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.updated", map[string]any{
		"provider_id": item.ID, "provider_type": item.ProviderType, "name": item.Name, "status": item.Status,
	})
	writeJSON(w, http.StatusOK, backupProviderToResponse(item))
}

func (s backupHandlers) enableProvider(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request enableBackupProviderRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	defer clearStringReferences(&request.CurrentPassword)
	if request.CurrentPassword == "" {
		writeError(w, http.StatusBadRequest, "current database password is required")
		return
	}
	if err := dbpkg.ValidateEncrypted(runtime.path, request.CurrentPassword); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid current database password")
		return
	}
	if err := validateRemoteBackupPassword(request.CurrentPassword, s.activeDatabaseDisplayName()); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	store := backups.NewStore(runtime.database)
	provider, err := store.GetProvider(r.Context(), id)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	client, err := backupServiceClient(runtime, provider)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	info, err := client.Info(r.Context())
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	public := cloneJSONMap(provider.Public)
	public["service_version"] = info.Version
	public["protocol_version"] = info.ProtocolVersion
	item, err := store.UpdateProvider(r.Context(), id, backups.UpdateProviderRequest{
		Name: provider.Name, Status: "active", Public: public,
	})
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	if err := store.UpdateLastChecked(r.Context(), id, time.Now()); err != nil {
		handleBackupProviderError(w, err)
		return
	}
	item, err = store.GetProvider(r.Context(), id)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.enabled", map[string]any{
		"provider_id": item.ID, "provider_type": item.ProviderType, "service_version": info.Version,
	})
	writeJSON(w, http.StatusOK, backupProviderToResponse(item))
}

func (s backupHandlers) testProvider(w http.ResponseWriter, r *http.Request) {
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
	client, err := backupServiceClient(runtime, provider)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	info, err := client.Info(r.Context())
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	checkedAt := time.Now().UTC()
	if err := store.UpdateLastChecked(r.Context(), id, checkedAt); err != nil {
		handleBackupProviderError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.tested", map[string]any{
		"provider_id": provider.ID, "provider_type": provider.ProviderType, "service_version": info.Version,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "checked_at": checkedAt.Format(time.RFC3339), "service_version": info.Version,
		"protocol_version": info.ProtocolVersion, "max_upload_bytes": info.MaxUploadBytes,
	})
}

func (s backupHandlers) deleteProvider(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := backups.NewStore(runtime.database).ArchiveProvider(r.Context(), id); err != nil {
		handleBackupProviderError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.archived", map[string]any{"provider_id": id})
	w.WriteHeader(http.StatusNoContent)
}

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
	if provider.Status == "active" {
		if err := syncBackupServiceRecords(r.Context(), runtime, store, provider); err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{"items": responses, "remote_sync": provider.Status == "active"})
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
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.uploaded", map[string]any{
		"provider_id": provider.ID, "provider_type": provider.ProviderType,
		"provider_file_id": backup.ID, "filename": backup.Filename, "size_bytes": backup.SizeBytes,
	})
	writeJSON(w, http.StatusCreated, backupRecordToResponse(record))
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
	s.installImportedDatabase(w, r, request.DatabaseName, request.DatabasePassword, copyBackupFile(tmpPath))
}

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

func encryptBackupServiceToken(runtime *databaseRuntime, secret map[string]any, required bool) (string, error) {
	token := stringFromMap(secret, "token")
	if token == "" {
		if required {
			return "", backups.ValidationError("backup service token is required")
		}
		return "", nil
	}
	if _, err := backups.NewServiceClient("http://localhost", token); err != nil {
		return "", err
	}
	return runtime.vault.EncryptJSON(map[string]any{"token": token})
}

func decryptBackupProviderSecret(runtime *databaseRuntime, provider backups.Provider) (map[string]any, error) {
	if provider.EncryptedSecretJSON == "" {
		return map[string]any{}, nil
	}
	secrets := map[string]any{}
	if err := runtime.vault.DecryptJSON(provider.EncryptedSecretJSON, &secrets); err != nil {
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

func syncBackupServiceRecords(ctx context.Context, runtime *databaseRuntime, store *backups.Store, provider backups.Provider) error {
	client, err := backupServiceClient(runtime, provider)
	if err != nil {
		return err
	}
	items, err := client.ListBackups(ctx, stringFromMap(provider.Public, "stream_id"))
	if err != nil {
		return err
	}
	for _, item := range items {
		if _, err := upsertServiceBackupRecord(ctx, runtime, store, provider, item); err != nil {
			return err
		}
	}
	return store.UpdateLastChecked(ctx, provider.ID, time.Now())
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
	tmpPath := filepath.Join(filepath.Dir(runtime.path), fmt.Sprintf(".remote-backup-%d-%d.aipdb", provider.ID, time.Now().UnixNano()))
	downloaded, err := client.Download(ctx, stringFromMap(provider.Public, "stream_id"), record.ProviderFileID, tmpPath, maxImportBodyBytes)
	if err != nil {
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
			writeError(w, http.StatusNotFound, "remote backup version was not found")
		case http.StatusRequestEntityTooLarge:
			writeError(w, http.StatusRequestEntityTooLarge, "backup exceeds the remote service upload limit")
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
