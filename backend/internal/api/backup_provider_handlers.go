package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/backups"
)

const maxBackupProviderJSONBytes = 16 << 10

const (
	googleDeviceCodeURL  = "https://oauth2.googleapis.com/device/code"
	googleTokenURL       = "https://oauth2.googleapis.com/token"
	googleDriveScope     = "https://www.googleapis.com/auth/drive.file"
	googleDriveFilesURL  = "https://www.googleapis.com/drive/v3/files"
	googleDriveUploadURL = "https://www.googleapis.com/upload/drive/v3/files"
)

type backupProviderRequest struct {
	ProviderType string         `json:"provider_type"`
	Name         string         `json:"name"`
	Status       string         `json:"status,omitempty"`
	Public       map[string]any `json:"public,omitempty"`
	Secret       map[string]any `json:"secret,omitempty"`
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
	ID                   int64          `json:"id"`
	ProviderType         string         `json:"provider_type"`
	Name                 string         `json:"name"`
	Status               string         `json:"status"`
	Public               map[string]any `json:"public,omitempty"`
	HasSecret            bool           `json:"has_secret"`
	HasOAuthClientSecret bool           `json:"has_oauth_client_secret,omitempty"`
	HasOAuthToken        bool           `json:"has_oauth_token,omitempty"`
	LastCheckedAt        *string        `json:"last_checked_at,omitempty"`
	CreatedAt            string         `json:"created_at"`
	UpdatedAt            string         `json:"updated_at"`
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

type backupOAuthFlow struct {
	ProviderID   int64
	ClientID     string
	ClientSecret string
	DeviceCode   string
	ExpiresAt    time.Time
	Interval     int
}

type googleDeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURL         string `json:"verification_url"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURLComplete string `json:"verification_url_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
	Error                   string `json:"error"`
	ErrorDescription        string `json:"error_description"`
}

type googleTokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	Scope            string `json:"scope"`
	TokenType        string `json:"token_type"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type googleDriveFilesResponse struct {
	Files []googleDriveFile `json:"files"`
	Error googleAPIError    `json:"error"`
}

type googleDriveFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        string `json:"size"`
	WebViewLink string `json:"webViewLink"`
}

type googleAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func (s backupHandlers) providerCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.activeRuntimeOrLocked(w); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": []backupProviderCatalogItem{
			{
				ProviderType: "google_drive",
				Label:        "Google Drive",
				Status:       "metadata_ready",
				Capabilities: []string{"encrypted_secret_storage", "encrypted_database_upload", "backup_record_metadata"},
			},
		},
	})
}

func (s backupHandlers) listProviders(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	store := backups.NewStore(runtime.database)
	items, err := store.ListProviders(r.Context())
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	responses := make([]backupProviderResponse, 0, len(items))
	for _, item := range items {
		response, err := backupProviderToResponse(runtime, item)
		if err != nil {
			writeInternalError(w)
			return
		}
		responses = append(responses, response)
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
	encrypted, err := encryptBackupProviderSecret(runtime, request.Secret)
	if err != nil {
		writeInternalError(w)
		return
	}
	store := backups.NewStore(runtime.database)
	item, err := store.CreateProvider(r.Context(), backups.CreateProviderRequest{
		ProviderType: strings.TrimSpace(request.ProviderType),
		Name:         strings.TrimSpace(request.Name),
		Public:       request.Public,
		Encrypted:    encrypted,
	})
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.created", map[string]any{
		"provider_id":   item.ID,
		"provider_type": item.ProviderType,
		"name":          item.Name,
	})
	response, err := backupProviderToResponse(runtime, item)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, response)
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
	public := request.Public
	if public == nil {
		existing, err := store.GetProvider(r.Context(), id)
		if err != nil {
			handleBackupProviderError(w, err)
			return
		}
		public = existing.Public
	}
	var encrypted *string
	if request.Secret != nil {
		value, err := encryptBackupProviderSecret(runtime, request.Secret)
		if err != nil {
			writeInternalError(w)
			return
		}
		encrypted = &value
	}
	item, err := store.UpdateProvider(r.Context(), id, backups.UpdateProviderRequest{
		Name:      strings.TrimSpace(request.Name),
		Status:    strings.TrimSpace(request.Status),
		Public:    public,
		Encrypted: encrypted,
	})
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.updated", map[string]any{
		"provider_id":   item.ID,
		"provider_type": item.ProviderType,
		"name":          item.Name,
		"status":        item.Status,
	})
	response, err := backupProviderToResponse(runtime, item)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, response)
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
	store := backups.NewStore(runtime.database)
	if err := store.ArchiveProvider(r.Context(), id); err != nil {
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
	if _, err := store.GetProvider(r.Context(), id); err != nil {
		handleBackupProviderError(w, err)
		return
	}
	records, err := store.ListRecords(r.Context(), backups.ListRecordsFilter{
		ProviderID:     id,
		DatabaseName:   strings.TrimSpace(r.URL.Query().Get("database_name")),
		IncludeDeleted: strings.TrimSpace(r.URL.Query().Get("include_deleted")) == "true",
	})
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	responses := make([]backupRecordResponse, 0, len(records))
	for _, item := range records {
		responses = append(responses, backupRecordToResponse(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": responses})
}

func (s backupHandlers) uploadProviderBackup(w http.ResponseWriter, r *http.Request) {
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
	if provider.ProviderType != "google_drive" {
		writeError(w, http.StatusBadRequest, "backup provider upload is only implemented for Google Drive")
		return
	}
	secrets, err := decryptBackupProviderSecret(runtime, provider)
	if err != nil {
		writeInternalError(w)
		return
	}
	accessToken, updatedSecrets, err := s.googleAccessToken(r.Context(), runtime, store, provider, secrets)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if updatedSecrets != nil {
		secrets = updatedSecrets
	}
	s.lifecycleMu.RLock()
	snapshot, err := createDatabaseSnapshot(runtime)
	if err != nil {
		s.lifecycleMu.RUnlock()
		writeInternalError(w)
		return
	}
	s.lifecycleMu.RUnlock()
	defer os.Remove(snapshot.Path)

	sizeBytes, checksum, err := fileSizeAndChecksum(snapshot.Path)
	if err != nil {
		writeInternalError(w)
		return
	}
	folderID, err := ensureGoogleDriveFolder(r.Context(), accessToken, strings.TrimSpace(stringFromMap(provider.Public, "folder_name")))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	driveFile, err := uploadGoogleDriveFile(r.Context(), accessToken, folderID, snapshot.Filename, snapshot.Path, sizeBytes)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	hostname, _ := os.Hostname()
	uploadedAt := time.Now().UTC()
	record, err := store.CreateRecord(r.Context(), backups.CreateRecordRequest{
		ProviderID:      provider.ID,
		DatabaseID:      runtime.id,
		DatabaseName:    backupDatabaseName(runtime.id),
		ProviderFileID:  driveFile.ID,
		Filename:        snapshot.Filename,
		SourceMachine:   hostname,
		SizeBytes:       sizeBytes,
		ChecksumSHA256:  checksum,
		BackupCreatedAt: snapshot.CreatedAt.Format(time.RFC3339),
		UploadedAt:      uploadedAt.Format(time.RFC3339),
		Metadata: map[string]any{
			"provider_type":       provider.ProviderType,
			"drive_folder_id":     folderID,
			"drive_file_name":     driveFile.Name,
			"drive_web_view_link": driveFile.WebViewLink,
			"token_refreshed":     updatedSecrets != nil,
		},
	})
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.uploaded", map[string]any{
		"provider_id":      provider.ID,
		"provider_type":    provider.ProviderType,
		"provider_file_id": driveFile.ID,
		"filename":         snapshot.Filename,
		"size_bytes":       sizeBytes,
	})
	writeJSON(w, http.StatusCreated, backupRecordToResponse(record))
}

func (s backupHandlers) downloadProviderRecord(w http.ResponseWriter, r *http.Request) {
	runtime, provider, record, accessToken, ok := s.resolveGoogleDriveRecord(w, r)
	if !ok {
		return
	}
	if record.SizeBytes > maxImportBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "backup is too large to download through the gateway")
		return
	}
	tmpPath, err := s.downloadGoogleDriveRecordToTemp(r.Context(), runtime, accessToken, record)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer os.Remove(tmpPath)
	if err := s.verifyBackupRecordFile(tmpPath, record); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.record.downloaded", map[string]any{
		"provider_id": provider.ID,
		"record_id":   record.ID,
		"filename":    record.Filename,
	})
	filename := strings.TrimSpace(record.Filename)
	if filename == "" {
		filename = backupDatabaseName(record.DatabaseID) + ".aipdb"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	http.ServeFile(w, r, tmpPath)
}

func (s backupHandlers) restoreProviderRecord(w http.ResponseWriter, r *http.Request) {
	runtime, provider, record, accessToken, ok := s.resolveGoogleDriveRecord(w, r)
	if !ok {
		return
	}
	var request restoreBackupRecordRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	defer clearStringReferences(&request.DatabasePassword)
	if record.SizeBytes > maxImportBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "backup is too large to restore through the gateway")
		return
	}
	tmpPath, err := s.downloadGoogleDriveRecordToTemp(r.Context(), runtime, accessToken, record)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer os.Remove(tmpPath)
	if err := s.verifyBackupRecordFile(tmpPath, record); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.record.restore_requested", map[string]any{
		"provider_id":    provider.ID,
		"record_id":      record.ID,
		"filename":       record.Filename,
		"database_name":  strings.TrimSpace(request.DatabaseName),
		"source_machine": record.SourceMachine,
	})
	s.installImportedDatabase(w, r, request.DatabaseName, request.DatabasePassword, func(targetPath string) error {
		source, err := os.Open(tmpPath)
		if err != nil {
			return err
		}
		defer source.Close()
		target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(target, source); err != nil {
			_ = target.Close()
			return err
		}
		return target.Close()
	})
}

func (s backupHandlers) startGoogleDeviceFlow(w http.ResponseWriter, r *http.Request) {
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
	if provider.ProviderType != "google_drive" {
		writeError(w, http.StatusBadRequest, "backup provider is not Google Drive")
		return
	}
	clientID := strings.TrimSpace(stringFromMap(provider.Public, "client_id"))
	if clientID == "" {
		writeError(w, http.StatusBadRequest, "google oauth client id is required before connecting")
		return
	}
	secrets, err := decryptBackupProviderSecret(runtime, provider)
	if err != nil {
		writeInternalError(w)
		return
	}
	clientSecret := strings.TrimSpace(stringFromMap(secrets, "client_secret"))
	if clientSecret == "" {
		writeError(w, http.StatusBadRequest, "google oauth client secret is required before connecting")
		return
	}
	response, err := requestGoogleDeviceCode(r.Context(), clientID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	interval := response.Interval
	if interval < 1 {
		interval = 5
	}
	expiresIn := response.ExpiresIn
	if expiresIn < 1 {
		expiresIn = 1800
	}
	s.backupOAuthMu.Lock()
	s.backupOAuthFlows[id] = backupOAuthFlow{
		ProviderID:   id,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		DeviceCode:   response.DeviceCode,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
		Interval:     interval,
	}
	s.backupOAuthMu.Unlock()
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.google_device_started", map[string]any{
		"provider_id": id,
	})
	verificationURL := response.VerificationURL
	if verificationURL == "" {
		verificationURL = response.VerificationURI
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"provider_id":               id,
		"user_code":                 response.UserCode,
		"verification_url":          verificationURL,
		"verification_url_complete": response.VerificationURLComplete,
		"expires_in":                expiresIn,
		"interval":                  interval,
	})
}

func (s backupHandlers) pollGoogleDeviceFlow(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	s.backupOAuthMu.Lock()
	flow, exists := s.backupOAuthFlows[id]
	s.backupOAuthMu.Unlock()
	if !exists {
		writeError(w, http.StatusConflict, "google device flow has not been started")
		return
	}
	if time.Now().After(flow.ExpiresAt) {
		s.backupOAuthMu.Lock()
		delete(s.backupOAuthFlows, id)
		s.backupOAuthMu.Unlock()
		writeError(w, http.StatusGone, "google device flow expired")
		return
	}
	store := backups.NewStore(runtime.database)
	provider, err := store.GetProvider(r.Context(), id)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	tokenResponse, pending, err := pollGoogleDeviceToken(r.Context(), flow)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if pending != "" {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":      pending,
			"retry_after": flow.Interval,
		})
		return
	}
	now := time.Now().UTC()
	expiresAt := ""
	if tokenResponse.ExpiresIn > 0 {
		expiresAt = now.Add(time.Duration(tokenResponse.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	secret, err := decryptBackupProviderSecret(runtime, provider)
	if err != nil {
		writeInternalError(w)
		return
	}
	secret["access_token"] = tokenResponse.AccessToken
	secret["refresh_token"] = tokenResponse.RefreshToken
	secret["token_type"] = tokenResponse.TokenType
	secret["scope"] = tokenResponse.Scope
	secret["obtained_at"] = now.Format(time.RFC3339)
	secret["expires_at"] = expiresAt
	encrypted, err := encryptBackupProviderSecret(runtime, secret)
	if err != nil {
		writeInternalError(w)
		return
	}
	updated, err := store.UpdateProvider(r.Context(), id, backups.UpdateProviderRequest{
		Name:      provider.Name,
		Status:    provider.Status,
		Public:    provider.Public,
		Encrypted: &encrypted,
	})
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	s.backupOAuthMu.Lock()
	delete(s.backupOAuthFlows, id)
	s.backupOAuthMu.Unlock()
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "backup.provider.google_connected", map[string]any{
		"provider_id": id,
		"scope":       tokenResponse.Scope,
	})
	response, err := backupProviderToResponse(runtime, updated)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func encryptBackupProviderSecret(runtime *databaseRuntime, secret map[string]any) (string, error) {
	if len(secret) == 0 {
		return "", nil
	}
	return runtime.vault.EncryptJSON(secret)
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

func validateBackupProviderPayload(w http.ResponseWriter, public map[string]any, secret map[string]any) bool {
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

func backupProviderToResponse(runtime *databaseRuntime, item backups.Provider) (backupProviderResponse, error) {
	response := backupProviderResponse{
		ID:            item.ID,
		ProviderType:  item.ProviderType,
		Name:          item.Name,
		Status:        item.Status,
		Public:        item.Public,
		HasSecret:     item.EncryptedSecretJSON != "",
		LastCheckedAt: item.LastCheckedAt,
		CreatedAt:     item.CreatedAt,
		UpdatedAt:     item.UpdatedAt,
	}
	if item.EncryptedSecretJSON == "" {
		return response, nil
	}
	secrets, err := decryptBackupProviderSecret(runtime, item)
	if err != nil {
		return backupProviderResponse{}, err
	}
	response.HasOAuthClientSecret = strings.TrimSpace(stringFromMap(secrets, "client_secret")) != ""
	response.HasOAuthToken = strings.TrimSpace(stringFromMap(secrets, "access_token")) != "" || strings.TrimSpace(stringFromMap(secrets, "refresh_token")) != ""
	return response, nil
}

func backupRecordToResponse(item backups.Record) backupRecordResponse {
	return backupRecordResponse{
		ID:              item.ID,
		ProviderID:      item.ProviderID,
		DatabaseID:      item.DatabaseID,
		DatabaseName:    item.DatabaseName,
		ProviderFileID:  item.ProviderFileID,
		Filename:        item.Filename,
		SourceMachine:   item.SourceMachine,
		SizeBytes:       item.SizeBytes,
		ChecksumSHA256:  item.ChecksumSHA256,
		BackupCreatedAt: item.BackupCreatedAt,
		UploadedAt:      item.UploadedAt,
		Metadata:        item.Metadata,
		DeletedAt:       item.DeletedAt,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
}

func handleBackupProviderError(w http.ResponseWriter, err error) {
	if errors.Is(err, backups.ErrNotFound) {
		writeError(w, http.StatusNotFound, "backup provider not found")
		return
	}
	if errors.Is(err, backups.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "backup record not found")
		return
	}
	var validation backups.ValidationError
	if errors.As(err, &validation) {
		writeError(w, http.StatusBadRequest, validation.Error())
		return
	}
	writeInternalError(w)
}

func (s backupHandlers) resolveGoogleDriveRecord(w http.ResponseWriter, r *http.Request) (*databaseRuntime, backups.Provider, backups.Record, string, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, backups.Provider{}, backups.Record{}, "", false
	}
	providerID, ok := parseID(w, r)
	if !ok {
		return nil, backups.Provider{}, backups.Record{}, "", false
	}
	recordID, ok := parsePathInt64(w, r, "record_id", "backup record id")
	if !ok {
		return nil, backups.Provider{}, backups.Record{}, "", false
	}
	store := backups.NewStore(runtime.database)
	provider, err := store.GetProvider(r.Context(), providerID)
	if err != nil {
		handleBackupProviderError(w, err)
		return nil, backups.Provider{}, backups.Record{}, "", false
	}
	if provider.ProviderType != "google_drive" {
		writeError(w, http.StatusBadRequest, "backup record restore is only implemented for Google Drive")
		return nil, backups.Provider{}, backups.Record{}, "", false
	}
	record, err := store.GetRecord(r.Context(), providerID, recordID)
	if err != nil {
		handleBackupProviderError(w, err)
		return nil, backups.Provider{}, backups.Record{}, "", false
	}
	secrets, err := decryptBackupProviderSecret(runtime, provider)
	if err != nil {
		writeInternalError(w)
		return nil, backups.Provider{}, backups.Record{}, "", false
	}
	accessToken, _, err := s.googleAccessToken(r.Context(), runtime, store, provider, secrets)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return nil, backups.Provider{}, backups.Record{}, "", false
	}
	return runtime, provider, record, accessToken, true
}

func (s backupHandlers) downloadGoogleDriveRecordToTemp(ctx context.Context, runtime *databaseRuntime, accessToken string, record backups.Record) (string, error) {
	tempDir := filepath.Dir(runtime.path)
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(tempDir, ".google-drive-backup-*.aipdb")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := downloadGoogleDriveFile(ctx, accessToken, record.ProviderFileID, tmp, maxImportBodyBytes); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return "", err
	}
	cleanup = false
	return tmpPath, nil
}

func (s backupHandlers) verifyBackupRecordFile(path string, record backups.Record) error {
	sizeBytes, checksum, err := fileSizeAndChecksum(path)
	if err != nil {
		return err
	}
	if record.SizeBytes > 0 && sizeBytes != record.SizeBytes {
		return fmt.Errorf("downloaded backup size mismatch")
	}
	if strings.TrimSpace(record.ChecksumSHA256) != "" && !strings.EqualFold(checksum, strings.TrimSpace(record.ChecksumSHA256)) {
		return fmt.Errorf("downloaded backup checksum mismatch")
	}
	return nil
}

func requestGoogleDeviceCode(ctx context.Context, clientID string) (googleDeviceCodeResponse, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", googleDriveScope)
	var result googleDeviceCodeResponse
	if err := postGoogleForm(ctx, googleDeviceCodeURL, form, &result); err != nil {
		return googleDeviceCodeResponse{}, err
	}
	if result.Error != "" {
		return googleDeviceCodeResponse{}, fmt.Errorf("google device flow failed: %s", firstNonEmpty(result.ErrorDescription, result.Error))
	}
	if result.DeviceCode == "" || result.UserCode == "" || (result.VerificationURL == "" && result.VerificationURI == "") {
		return googleDeviceCodeResponse{}, fmt.Errorf("google device flow returned an incomplete response")
	}
	return result, nil
}

func pollGoogleDeviceToken(ctx context.Context, flow backupOAuthFlow) (googleTokenResponse, string, error) {
	form := url.Values{}
	form.Set("client_id", flow.ClientID)
	form.Set("client_secret", flow.ClientSecret)
	form.Set("device_code", flow.DeviceCode)
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	var result googleTokenResponse
	if err := postGoogleForm(ctx, googleTokenURL, form, &result); err != nil {
		return googleTokenResponse{}, "", err
	}
	switch result.Error {
	case "":
		if result.AccessToken == "" {
			return googleTokenResponse{}, "", fmt.Errorf("google token response did not include an access token")
		}
		return result, "", nil
	case "authorization_pending", "slow_down":
		return googleTokenResponse{}, result.Error, nil
	case "expired_token":
		return googleTokenResponse{}, "", fmt.Errorf("google device flow expired")
	default:
		return googleTokenResponse{}, "", fmt.Errorf("google device flow failed: %s", firstNonEmpty(result.ErrorDescription, result.Error))
	}
}

func (s backupHandlers) googleAccessToken(ctx context.Context, runtime *databaseRuntime, store *backups.Store, provider backups.Provider, secrets map[string]any) (string, map[string]any, error) {
	accessToken := strings.TrimSpace(stringFromMap(secrets, "access_token"))
	refreshToken := strings.TrimSpace(stringFromMap(secrets, "refresh_token"))
	clientID := strings.TrimSpace(stringFromMap(provider.Public, "client_id"))
	clientSecret := strings.TrimSpace(stringFromMap(secrets, "client_secret"))
	if accessToken != "" && !googleTokenNeedsRefresh(stringFromMap(secrets, "expires_at")) {
		return accessToken, nil, nil
	}
	if refreshToken == "" || clientID == "" || clientSecret == "" {
		if accessToken == "" {
			return "", nil, fmt.Errorf("Google Drive is connected but no access token is stored; reconnect the provider")
		}
		return "", nil, fmt.Errorf("Google Drive token expired and cannot be refreshed; reconnect the provider")
	}
	tokenResponse, err := refreshGoogleAccessToken(ctx, clientID, clientSecret, refreshToken)
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC()
	expiresAt := ""
	if tokenResponse.ExpiresIn > 0 {
		expiresAt = now.Add(time.Duration(tokenResponse.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	secrets["access_token"] = tokenResponse.AccessToken
	if tokenResponse.RefreshToken != "" {
		secrets["refresh_token"] = tokenResponse.RefreshToken
	}
	secrets["token_type"] = firstNonEmpty(tokenResponse.TokenType, stringFromMap(secrets, "token_type"))
	secrets["scope"] = firstNonEmpty(tokenResponse.Scope, stringFromMap(secrets, "scope"))
	secrets["obtained_at"] = now.Format(time.RFC3339)
	secrets["expires_at"] = expiresAt
	encrypted, err := encryptBackupProviderSecret(runtime, secrets)
	if err != nil {
		return "", nil, err
	}
	if _, err := store.UpdateProvider(ctx, provider.ID, backups.UpdateProviderRequest{
		Name:      provider.Name,
		Status:    provider.Status,
		Public:    provider.Public,
		Encrypted: &encrypted,
	}); err != nil {
		return "", nil, err
	}
	return tokenResponse.AccessToken, secrets, nil
}

func googleTokenNeedsRefresh(expiresAt string) bool {
	expiresAt = strings.TrimSpace(expiresAt)
	if expiresAt == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return true
	}
	return time.Now().UTC().Add(90 * time.Second).After(parsed)
}

func refreshGoogleAccessToken(ctx context.Context, clientID string, clientSecret string, refreshToken string) (googleTokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")
	var result googleTokenResponse
	if err := postGoogleForm(ctx, googleTokenURL, form, &result); err != nil {
		return googleTokenResponse{}, err
	}
	if result.Error != "" {
		return googleTokenResponse{}, fmt.Errorf("google token refresh failed: %s", firstNonEmpty(result.ErrorDescription, result.Error))
	}
	if result.AccessToken == "" {
		return googleTokenResponse{}, fmt.Errorf("google token refresh did not include an access token")
	}
	return result, nil
}

func ensureGoogleDriveFolder(ctx context.Context, accessToken string, folderName string) (string, error) {
	folderName = strings.TrimSpace(folderName)
	if folderName == "" {
		folderName = "AIPermission Backups"
	}
	params := url.Values{}
	params.Set("q", fmt.Sprintf("name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", googleDriveQueryLiteral(folderName)))
	params.Set("pageSize", "1")
	params.Set("fields", "files(id,name)")
	var list googleDriveFilesResponse
	if err := googleDriveJSON(ctx, http.MethodGet, googleDriveFilesURL+"?"+params.Encode(), accessToken, nil, &list); err != nil {
		return "", err
	}
	if len(list.Files) > 0 && strings.TrimSpace(list.Files[0].ID) != "" {
		return list.Files[0].ID, nil
	}
	payload := map[string]any{
		"name":     folderName,
		"mimeType": "application/vnd.google-apps.folder",
	}
	var created googleDriveFile
	if err := googleDriveJSON(ctx, http.MethodPost, googleDriveFilesURL+"?fields=id,name", accessToken, payload, &created); err != nil {
		return "", err
	}
	if strings.TrimSpace(created.ID) == "" {
		return "", fmt.Errorf("Google Drive folder creation returned no id")
	}
	return created.ID, nil
}

func uploadGoogleDriveFile(ctx context.Context, accessToken string, folderID string, filename string, path string, sizeBytes int64) (googleDriveFile, error) {
	metadata := map[string]any{"name": filename}
	if strings.TrimSpace(folderID) != "" {
		metadata["parents"] = []string{folderID}
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		return googleDriveFile{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	startRequest, err := http.NewRequestWithContext(requestCtx, http.MethodPost, googleDriveUploadURL+"?uploadType=resumable&fields=id,name,size,webViewLink", bytes.NewReader(body))
	if err != nil {
		return googleDriveFile{}, err
	}
	startRequest.Header.Set("Authorization", "Bearer "+accessToken)
	startRequest.Header.Set("Content-Type", "application/json; charset=UTF-8")
	startRequest.Header.Set("X-Upload-Content-Type", "application/octet-stream")
	startRequest.Header.Set("X-Upload-Content-Length", fmt.Sprintf("%d", sizeBytes))
	startResponse, err := http.DefaultClient.Do(startRequest)
	if err != nil {
		return googleDriveFile{}, fmt.Errorf("start Google Drive upload: %w", err)
	}
	defer startResponse.Body.Close()
	if startResponse.StatusCode < 200 || startResponse.StatusCode > 299 {
		return googleDriveFile{}, googleHTTPError(startResponse, "start Google Drive upload")
	}
	location := strings.TrimSpace(startResponse.Header.Get("Location"))
	if location == "" {
		return googleDriveFile{}, fmt.Errorf("Google Drive upload session did not return a location")
	}
	file, err := os.Open(path)
	if err != nil {
		return googleDriveFile{}, err
	}
	defer file.Close()
	uploadCtx, uploadCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer uploadCancel()
	uploadRequest, err := http.NewRequestWithContext(uploadCtx, http.MethodPut, location, file)
	if err != nil {
		return googleDriveFile{}, err
	}
	uploadRequest.Header.Set("Content-Type", "application/octet-stream")
	uploadRequest.ContentLength = sizeBytes
	uploadResponse, err := http.DefaultClient.Do(uploadRequest)
	if err != nil {
		return googleDriveFile{}, fmt.Errorf("upload Google Drive file: %w", err)
	}
	defer uploadResponse.Body.Close()
	if uploadResponse.StatusCode < 200 || uploadResponse.StatusCode > 299 {
		return googleDriveFile{}, googleHTTPError(uploadResponse, "upload Google Drive file")
	}
	var result googleDriveFile
	if err := json.NewDecoder(io.LimitReader(uploadResponse.Body, 64<<10)).Decode(&result); err != nil {
		return googleDriveFile{}, fmt.Errorf("parse Google Drive upload response: %w", err)
	}
	if strings.TrimSpace(result.ID) == "" {
		return googleDriveFile{}, fmt.Errorf("Google Drive upload response returned no file id")
	}
	return result, nil
}

func downloadGoogleDriveFile(ctx context.Context, accessToken string, fileID string, target *os.File, maxBytes int64) error {
	if strings.TrimSpace(fileID) == "" {
		return fmt.Errorf("backup record is missing a Google Drive file id")
	}
	downloadCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	endpoint := googleDriveFilesURL + "/" + url.PathEscape(fileID) + "?alt=media"
	request, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("download Google Drive backup: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return googleHTTPError(response, "download Google Drive backup")
	}
	written, err := io.Copy(target, io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("write Google Drive backup: %w", err)
	}
	if written > maxBytes {
		return fmt.Errorf("downloaded backup is too large; maximum import size is 256 MiB")
	}
	return nil
}

func googleDriveJSON(ctx context.Context, method string, endpoint string, accessToken string, payload any, target any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, method, endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("Google Drive request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return googleHTTPError(response, "Google Drive request")
	}
	if target == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(target); err != nil {
		return fmt.Errorf("parse Google Drive response: %w", err)
	}
	return nil
}

func googleHTTPError(response *http.Response, prefix string) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	var parsed struct {
		Error googleAPIError `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return fmt.Errorf("%s failed with %s: %s", prefix, response.Status, parsed.Error.Message)
	}
	text := strings.TrimSpace(string(body))
	if text != "" {
		return fmt.Errorf("%s failed with %s: %s", prefix, response.Status, text)
	}
	return fmt.Errorf("%s failed with %s", prefix, response.Status)
}

func fileSizeAndChecksum(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

func googleDriveQueryLiteral(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return value
}

func backupDatabaseName(databaseID string) string {
	databaseID = strings.TrimSpace(databaseID)
	if databaseID == "" || databaseID == "default" {
		return "Default"
	}
	if databaseID == "local-default" {
		return "Local Default"
	}
	return strings.ReplaceAll(databaseID, "-", " ")
}

func postGoogleForm(ctx context.Context, endpoint string, form url.Values, target any) error {
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("google oauth request failed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("read google oauth response: %w", err)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("parse google oauth response: %w", err)
	}
	if response.StatusCode >= 500 {
		return fmt.Errorf("google oauth request failed with %s", response.Status)
	}
	return nil
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
