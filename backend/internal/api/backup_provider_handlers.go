package api

import (
	"net/http"
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

type pruneBackupProviderRequest struct {
	KeepLatest int `json:"keep_latest"`
}

type updateBackupRetentionRequest struct {
	Enabled    bool `json:"enabled"`
	KeepLatest int  `json:"keep_latest"`
	ApplyNow   bool `json:"apply_now"`
}

type deleteBackupRecordsRequest struct {
	RecordIDs []int64 `json:"record_ids"`
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

type backupFreshnessResponse struct {
	ProviderID         int64  `json:"provider_id"`
	ProviderName       string `json:"provider_name"`
	RemoteNewer        bool   `json:"remote_newer"`
	LatestRemoteID     string `json:"latest_remote_id,omitempty"`
	LatestRemoteAt     string `json:"latest_remote_at,omitempty"`
	LatestRemoteSource string `json:"latest_remote_source,omitempty"`
	LatestKnownID      string `json:"latest_known_id,omitempty"`
	LatestKnownAt      string `json:"latest_known_at,omitempty"`
}

type backupSyncResult struct {
	Freshness backupFreshnessResponse
}

func (s backupHandlers) providerCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items": []backupProviderCatalogItem{{
			ProviderType: backups.ServiceProviderType,
			Label:        "AIPermission Backup",
			Status:       "available",
			Capabilities: []string{"encrypted_database_upload", "immutable_versions", "prune_versions", "delete_versions", "storage_usage", "automatic_retention", "first_run_restore", "self_hosted"},
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
