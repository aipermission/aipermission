package connectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httpattachment"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const MaxProfileRestoreBodyBytes = 256 << 20

type ProfileBackupScope struct {
	Database *sql.DB
	Registry *connectors.Registry
	Runtime  CredentialRuntimePorts
	Observe  func(context.Context, string, map[string]any)
}

type ProfileBackupScopeProvider func(http.ResponseWriter) (ProfileBackupScope, bool)

type ProfileBackupHTTPHandler struct{ scope ProfileBackupScopeProvider }

func NewProfileBackupHTTPHandler(scope ProfileBackupScopeProvider) *ProfileBackupHTTPHandler {
	return &ProfileBackupHTTPHandler{scope: scope}
}

func (h *ProfileBackupHTTPHandler) Download(w http.ResponseWriter, r *http.Request) {
	resolved, ok := h.resolveProfile(w, r)
	if !ok {
		return
	}
	backupRestorer, ok := resolved.connector.(connectors.BackupRestorer)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "connector does not support backup")
		return
	}
	artifact, err := backupRestorer.Backup(r.Context(), resolved.runtime, connectors.BackupRequest{Format: "sql"})
	if err != nil {
		writeProvisionError(w, err, resolved.boundary.Redact(resolved.scope.Runtime.RedactText(r.Context(), err.Error())))
		return
	}
	if len(artifact.Data) == 0 {
		httptransport.WriteError(w, http.StatusBadRequest, "connector returned an empty backup")
		return
	}
	filename := httpattachment.SafeFilename(artifact.Filename, "connector-backup.sql")
	contentType := strings.TrimSpace(artifact.ContentType)
	if contentType == "" {
		contentType = "application/sql"
	}
	httpattachment.SetHeaders(w, filename, contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact.Data)
	resolved.observe(r, "connector.profile.backup.downloaded", filename)
}

func (h *ProfileBackupHTTPHandler) Restore(w http.ResponseWriter, r *http.Request) {
	resolved, ok := h.resolveProfile(w, r)
	if !ok {
		return
	}
	backupRestorer, ok := resolved.connector.(connectors.BackupRestorer)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "connector does not support restore")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxProfileRestoreBodyBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httptransport.WriteError(w, http.StatusRequestEntityTooLarge, "uploaded restore file is too large; maximum restore size is 256 MiB")
			return
		}
		httptransport.WriteError(w, http.StatusBadRequest, "invalid multipart restore upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if strings.TrimSpace(r.FormValue("confirm_target")) != resolved.target.Name {
		httptransport.WriteError(w, http.StatusBadRequest, "type the connector target name exactly to confirm restore")
		return
	}
	file, header, err := r.FormFile("dump")
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, "restore SQL file is required")
		return
	}
	defer file.Close()
	if header.Size == 0 {
		httptransport.WriteError(w, http.StatusBadRequest, "restore SQL file is empty")
		return
	}
	result, err := backupRestorer.Restore(r.Context(), resolved.runtime, connectors.RestoreRequest{
		Filename: header.Filename, Content: file, Size: header.Size,
	})
	if err != nil {
		writeProvisionError(w, err, resolved.boundary.Redact(resolved.scope.Runtime.RedactText(r.Context(), err.Error())))
		return
	}
	result, err = resolved.scope.Runtime.RedactResult(r.Context(), result, resolved.boundary)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	filename := httpattachment.SafeFilename(header.Filename, "restore.sql")
	resolved.observe(r, "connector.profile.backup.restored", filename)
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"result": result})
}

type resolvedProfileBackup struct {
	scope     ProfileBackupScope
	target    connectortargets.Target
	profile   connectortargets.CredentialProfile
	connector connectors.Connector
	runtime   connectors.RuntimeContext
	boundary  actionresult.CredentialBoundary
}

func (h *ProfileBackupHTTPHandler) resolveProfile(w http.ResponseWriter, r *http.Request) (resolvedProfileBackup, bool) {
	scope, ok := h.resolve(w)
	if !ok {
		return resolvedProfileBackup{}, false
	}
	targetID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return resolvedProfileBackup{}, false
	}
	profileID, ok := httptransport.ParsePathInt64(w, r, "profile_id", "profile_id is required")
	if !ok {
		return resolvedProfileBackup{}, false
	}
	store := connectortargets.NewStore(scope.Database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		writeTargetError(w, err)
		return resolvedProfileBackup{}, false
	}
	profile, err := store.GetCredentialProfile(r.Context(), targetID, profileID)
	if err != nil {
		writeTargetError(w, err)
		return resolvedProfileBackup{}, false
	}
	connector, ok := scope.Registry.Get(target.ConnectorKind)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
		return resolvedProfileBackup{}, false
	}
	secrets, err := decryptCredentialSecrets(r.Context(), profile, scope.Runtime.DecryptSecret)
	if err != nil {
		httptransport.WriteInternalError(w)
		return resolvedProfileBackup{}, false
	}
	boundary := actionresult.NewCredentialBoundary(secrets)
	return resolvedProfileBackup{
		scope: scope, target: target, profile: profile, connector: connector,
		runtime: scope.Runtime.RuntimeContext(target, profile, secrets, boundary), boundary: boundary,
	}, true
}

func (h *ProfileBackupHTTPHandler) resolve(w http.ResponseWriter) (ProfileBackupScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return ProfileBackupScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return ProfileBackupScope{}, false
	}
	if scope.Database == nil || scope.Registry == nil || !scope.Runtime.valid() || scope.Observe == nil {
		httptransport.WriteInternalError(w)
		return ProfileBackupScope{}, false
	}
	return scope, true
}

func (resolved resolvedProfileBackup) observe(r *http.Request, action, filename string) {
	resolved.scope.Observe(r.Context(), action, map[string]any{
		"target_id": resolved.target.ID, "profile_id": resolved.profile.ID,
		"connector_kind": resolved.target.ConnectorKind, "filename": filename,
	})
}
