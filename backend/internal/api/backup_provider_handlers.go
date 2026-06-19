package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/backups"
)

const maxBackupProviderJSONBytes = 16 << 10

type backupProviderRequest struct {
	ProviderType string         `json:"provider_type"`
	Name         string         `json:"name"`
	Status       string         `json:"status,omitempty"`
	Public       map[string]any `json:"public,omitempty"`
	Secret       map[string]any `json:"secret,omitempty"`
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
				Capabilities: []string{"encrypted_secret_storage", "backup_record_metadata"},
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
	writeJSON(w, http.StatusOK, backupProviderToResponse(item))
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

func encryptBackupProviderSecret(runtime *databaseRuntime, secret map[string]any) (string, error) {
	if len(secret) == 0 {
		return "", nil
	}
	return runtime.vault.EncryptJSON(secret)
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

func backupProviderToResponse(item backups.Provider) backupProviderResponse {
	return backupProviderResponse{
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
	var validation backups.ValidationError
	if errors.As(err, &validation) {
		writeError(w, http.StatusBadRequest, validation.Error())
		return
	}
	writeInternalError(w)
}
