package api

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

const maxConnectorRestoreBodyBytes = 256 << 20

func (s connectorTargetHandlers) downloadConnectorProfileBackup(w http.ResponseWriter, r *http.Request) {
	resolved, ok := s.resolveConnectorProfileRuntime(w, r)
	if !ok {
		return
	}
	backupRestorer, ok := resolved.connector.(connectors.BackupRestorer)
	if !ok {
		writeError(w, http.StatusBadRequest, "connector does not support backup")
		return
	}
	artifact, err := backupRestorer.Backup(r.Context(), resolved.runtimeContext, connectors.BackupRequest{Format: "sql"})
	if err != nil {
		handleConnectorProvisionError(w, err)
		return
	}
	if len(artifact.Data) == 0 {
		writeError(w, http.StatusBadRequest, "connector returned an empty backup")
		return
	}
	filename := safeDownloadFilename(artifact.Filename, "connector-backup.sql")
	contentType := strings.TrimSpace(artifact.ContentType)
	if contentType == "" {
		contentType = "application/sql"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact.Data)
	s.writeAudit(r.Context(), resolved.runtime, "user", nil, 0, "connector.profile.backup.downloaded", map[string]any{
		"target_id":      resolved.target.ID,
		"profile_id":     resolved.profile.ID,
		"connector_kind": resolved.target.ConnectorKind,
		"filename":       filename,
	})
}

func (s connectorTargetHandlers) restoreConnectorProfileBackup(w http.ResponseWriter, r *http.Request) {
	resolved, ok := s.resolveConnectorProfileRuntime(w, r)
	if !ok {
		return
	}
	backupRestorer, ok := resolved.connector.(connectors.BackupRestorer)
	if !ok {
		writeError(w, http.StatusBadRequest, "connector does not support restore")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxConnectorRestoreBodyBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "uploaded restore file is too large; maximum restore size is 256 MiB")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid multipart restore upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if strings.TrimSpace(r.FormValue("confirm_target")) != resolved.target.Name {
		writeError(w, http.StatusBadRequest, "type the connector target name exactly to confirm restore")
		return
	}
	file, header, err := r.FormFile("dump")
	if err != nil {
		writeError(w, http.StatusBadRequest, "restore SQL file is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeInternalError(w)
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "restore SQL file is empty")
		return
	}
	result, err := backupRestorer.Restore(r.Context(), resolved.runtimeContext, connectors.RestoreRequest{
		Filename: header.Filename,
		Data:     data,
	})
	if err != nil {
		handleConnectorProvisionError(w, err)
		return
	}
	s.writeAudit(r.Context(), resolved.runtime, "user", nil, 0, "connector.profile.backup.restored", map[string]any{
		"target_id":      resolved.target.ID,
		"profile_id":     resolved.profile.ID,
		"connector_kind": resolved.target.ConnectorKind,
		"filename":       safeDownloadFilename(header.Filename, "restore.sql"),
	})
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

type resolvedConnectorProfileRuntime struct {
	runtime        *databaseRuntime
	target         connectortargets.Target
	profile        connectortargets.CredentialProfile
	connector      connectors.Connector
	runtimeContext connectors.RuntimeContext
}

func (s connectorTargetHandlers) resolveConnectorProfileRuntime(w http.ResponseWriter, r *http.Request) (resolvedConnectorProfileRuntime, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return resolvedConnectorProfileRuntime{}, false
	}
	targetID, ok := parseID(w, r)
	if !ok {
		return resolvedConnectorProfileRuntime{}, false
	}
	profileID, ok := parsePathInt64(w, r, "profile_id", "profile_id")
	if !ok {
		return resolvedConnectorProfileRuntime{}, false
	}
	store := connectortargets.NewStore(runtime.database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return resolvedConnectorProfileRuntime{}, false
	}
	profile, err := store.GetCredentialProfile(r.Context(), targetID, profileID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return resolvedConnectorProfileRuntime{}, false
	}
	connector, ok := runtime.connectorRegistry().Get(target.ConnectorKind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return resolvedConnectorProfileRuntime{}, false
	}
	secrets, ok := s.decryptConnectorProfileSecrets(w, runtime, profile)
	if !ok {
		return resolvedConnectorProfileRuntime{}, false
	}
	return resolvedConnectorProfileRuntime{
		runtime:   runtime,
		target:    target,
		profile:   profile,
		connector: connector,
		runtimeContext: connectors.RuntimeContext{
			Target:       connectorTargetViewForProfile(target, profile.ID),
			Profile:      connectortargets.CredentialProfileView(profile),
			Secrets:      connectorSecretAccessor{values: secrets},
			Events:       noopConnectorEventSink{},
			Capabilities: connectorRuntimeCapabilitiesFor(target.ConnectorKind, s.Server, runtime),
		},
	}, true
}

func safeDownloadFilename(value string, fallback string) string {
	name := strings.TrimSpace(filepath.Base(value))
	if name == "." || name == "/" || name == "" {
		return fallback
	}
	name = strings.ReplaceAll(name, "\x00", "")
	if strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}
