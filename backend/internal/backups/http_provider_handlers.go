package backups

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const maxBackupProviderJSONBytes = 16 << 10

type backupProviderRequest struct {
	ProviderType string         `json:"provider_type"`
	Name         string         `json:"name"`
	Status       string         `json:"status,omitempty"`
	Public       map[string]any `json:"public,omitempty"`
	Secret       map[string]any `json:"secret,omitempty"`
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

type enableBackupProviderRequest struct {
	CurrentPassword string `json:"current_password"`
}

type backupProviderCatalogItem struct {
	ProviderType string   `json:"provider_type"`
	Label        string   `json:"label"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
}

type ProviderResponse struct {
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

type RecordResponse struct {
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

func (h *HTTPHandlers) ProviderCatalog(w http.ResponseWriter, _ *http.Request) {
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"items": []backupProviderCatalogItem{{
			ProviderType: ServiceProviderType,
			Label:        "AIPermission Backup",
			Status:       "available",
			Capabilities: []string{"encrypted_database_upload", "immutable_versions", "prune_versions", "delete_versions", "storage_usage", "automatic_retention", "first_run_restore", "self_hosted"},
		}},
	})
}

func (h *HTTPHandlers) ListProviders(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w, requireDatabase)
	if !ok {
		return
	}
	items, err := NewStore(runtime.Database).ListProviders(r.Context())
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	responses := make([]ProviderResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, ProviderToResponse(item))
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": responses})
}

func (h *HTTPHandlers) CreateProvider(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w, requireDatabase|requireProviderIdentity|requireSecrets|requireMutation)
	if !ok {
		return
	}
	var request backupProviderRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	if !validateBackupProviderPayload(w, request.Public, request.Secret) {
		return
	}
	if strings.TrimSpace(request.ProviderType) != ServiceProviderType {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported backup provider type")
		return
	}
	public, err := normalizeServiceProviderPublic(runtime, request.Public, nil)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	secret, err := backupServiceTokenSecret(request.Secret, true)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	var item Provider
	err = runtime.Mutate(
		r.Context(), "backup.provider.created",
		func() any { return backupProviderAuditPayload(item) },
		func(tx *sql.Tx) error {
			store := NewTxStore(tx)
			var err error
			item, err = store.CreateProvider(r.Context(), CreateProviderRequest{
				ProviderType: ServiceProviderType, Name: strings.TrimSpace(request.Name),
				Status: "disabled", Public: public, Encrypted: "",
			})
			if err != nil {
				return err
			}
			encrypted, err := runtime.Secrets.EncryptProviderSecret(item.ID, secret)
			if err != nil {
				return fmt.Errorf("encrypt backup provider secret: %w", err)
			}
			if err := store.SetProviderEncryptedSecret(r.Context(), item.ID, encrypted); err != nil {
				return err
			}
			item.EncryptedSecretJSON = encrypted
			return nil
		},
	)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusCreated, ProviderToResponse(item))
}

func (h *HTTPHandlers) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w, requireDatabase|requireProviderIdentity|requireSecrets|requireMutation)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	var request backupProviderRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	if !validateBackupProviderPayload(w, request.Public, request.Secret) {
		return
	}
	releaseProvider, ok := h.acquireProviderOperation(w, r.Context(), runtime.Database, id)
	if !ok {
		return
	}
	defer releaseProvider()
	store := NewStore(runtime.Database)
	existing, err := store.GetProvider(r.Context(), id)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	public, err := normalizeServiceProviderPublic(runtime, request.Public, existing.Public)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	status := strings.TrimSpace(request.Status)
	if status == "" {
		status = existing.Status
	}
	if existing.Status != "active" && status == "active" {
		httptransport.WriteError(w, http.StatusConflict, "enable this provider with the explicit enable action")
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = existing.Name
	}
	var encrypted *string
	if request.Secret != nil {
		secret, err := backupServiceTokenSecret(request.Secret, true)
		if err != nil {
			handleBackupProviderError(w, err)
			return
		}
		value, err := runtime.Secrets.EncryptProviderSecret(existing.ID, secret)
		if err != nil {
			httptransport.WriteInternalError(w)
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
	var item Provider
	err = runtime.Mutate(
		r.Context(), "backup.provider.updated",
		func() any { return backupProviderAuditPayload(item) },
		func(tx *sql.Tx) error {
			txStore := NewTxStore(tx)
			if stringFromMap(existing.Public, "base_url") != stringFromMap(public, "base_url") || encrypted != nil {
				if err := txStore.ExpireUnresolvedUploadOperations(r.Context(), id, "upload outcome expired because the backup service identity changed"); err != nil {
					return err
				}
			}
			var err error
			item, err = txStore.UpdateProvider(r.Context(), id, UpdateProviderRequest{
				Name: name, Status: status, Public: public, Encrypted: encrypted,
			})
			return err
		},
	)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, ProviderToResponse(item))
}

func (h *HTTPHandlers) TestProvider(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w, requireDatabase|requireSecrets|requireMutation)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	releaseProvider, ok := h.acquireProviderOperation(w, r.Context(), runtime.Database, id)
	if !ok {
		return
	}
	defer releaseProvider()
	store := NewStore(runtime.Database)
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
	err = runtime.Mutate(
		r.Context(), "backup.provider.tested",
		func() any {
			return map[string]any{
				"provider_id": provider.ID, "provider_type": provider.ProviderType,
				"service_version": info.Version,
			}
		},
		func(tx *sql.Tx) error { return NewTxStore(tx).UpdateLastChecked(r.Context(), id, checkedAt) },
	)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "checked_at": checkedAt.Format(time.RFC3339), "service_version": info.Version,
		"protocol_version": info.ProtocolVersion, "max_upload_bytes": info.MaxUploadBytes,
	})
}

func (h *HTTPHandlers) EnableProvider(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w, requireDatabase|requireProviderIdentity|requireSecrets|requireMutation|requirePasswordAuthorization)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	var request enableBackupProviderRequest
	if !httptransport.DecodeJSON(w, r, &request, maxBackupProviderJSONBytes) {
		return
	}
	defer func() { request.CurrentPassword = "" }()
	if request.CurrentPassword == "" {
		httptransport.WriteError(w, http.StatusBadRequest, "current database password is required")
		return
	}
	if !runtime.AuthorizePassword(w, r, request.CurrentPassword) {
		return
	}
	if err := ValidateRemoteBackupPassword(request.CurrentPassword, runtime.DatabaseName); err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	releaseProvider, ok := h.acquireProviderOperation(w, r.Context(), runtime.Database, id)
	if !ok {
		return
	}
	defer releaseProvider()
	item, err := EnableProvider(r.Context(), runtime, id)
	if err != nil {
		WriteProviderHTTPError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, ProviderToResponse(item))
}

func (h *HTTPHandlers) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w, requireDatabase|requireMutation)
	if !ok {
		return
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	releaseProvider, ok := h.acquireProviderOperation(w, r.Context(), runtime.Database, id)
	if !ok {
		return
	}
	defer releaseProvider()
	err := runtime.Mutate(
		r.Context(), "backup.provider.archived",
		func() any { return map[string]any{"provider_id": id} },
		func(tx *sql.Tx) error { return NewTxStore(tx).ArchiveProvider(r.Context(), id) },
	)
	if err != nil {
		handleBackupProviderError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func backupProviderAuditPayload(item Provider) map[string]any {
	return map[string]any{
		"provider_id": item.ID, "provider_type": item.ProviderType,
		"name": item.Name, "status": item.Status,
	}
}
