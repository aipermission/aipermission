package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/backups"
)

type transientBackupServiceRequest struct {
	BaseURL  string `json:"base_url"`
	Token    string `json:"token"`
	StreamID string `json:"stream_id,omitempty"`
}

type transientBackupRestoreRequest struct {
	BaseURL          string `json:"base_url"`
	Token            string `json:"token"`
	StreamID         string `json:"stream_id"`
	BackupID         string `json:"backup_id"`
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

type transientBackupStreamResponse struct {
	ID           string                  `json:"id"`
	DatabaseName string                  `json:"database_name"`
	Backups      []backups.ServiceBackup `json:"backups"`
}

func (s backupHandlers) listTransientRemoteBackups(w http.ResponseWriter, r *http.Request) {
	var request transientBackupServiceRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	defer clearStringReferences(&request.Token)
	client, err := backups.NewServiceClient(request.BaseURL, request.Token)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	if _, err := client.Info(r.Context()); err != nil {
		handleBackupServiceError(w, err)
		return
	}
	streams, err := client.ListStreams(r.Context())
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	if len(streams) > 100 {
		writeError(w, http.StatusBadGateway, "backup service returned too many streams for first-run restore")
		return
	}
	items := make([]transientBackupStreamResponse, 0, len(streams))
	for _, stream := range streams {
		if request.StreamID != "" && stream.ID != strings.TrimSpace(request.StreamID) {
			continue
		}
		var versions []backups.ServiceBackup
		if request.StreamID != "" {
			versions, err = client.ListBackups(r.Context(), stream.ID)
			if err != nil {
				handleBackupServiceError(w, err)
				return
			}
		}
		items = append(items, transientBackupStreamResponse{ID: stream.ID, DatabaseName: stream.DatabaseName, Backups: versions})
	}
	if request.StreamID != "" && len(items) == 0 {
		writeError(w, http.StatusNotFound, "remote backup stream was not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s backupHandlers) restoreTransientRemoteBackup(w http.ResponseWriter, r *http.Request) {
	var request transientBackupRestoreRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	defer clearStringReferences(&request.Token, &request.DatabasePassword)
	if strings.TrimSpace(request.DatabasePassword) == "" {
		writeError(w, http.StatusBadRequest, "database password is required")
		return
	}
	if strings.TrimSpace(request.DatabaseName) == "" {
		writeError(w, http.StatusBadRequest, "database name is required")
		return
	}
	client, err := backups.NewServiceClient(request.BaseURL, request.Token)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	if _, err := client.Info(r.Context()); err != nil {
		handleBackupServiceError(w, err)
		return
	}
	stream, version, err := findTransientRemoteBackup(r, client, request.StreamID, request.BackupID)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	if version.SizeBytes > maxImportBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "remote backup is too large to restore through the gateway")
		return
	}
	tmpPath := filepath.Join(filepath.Dir(s.config.DataPath), ".first-run-restore-"+time.Now().UTC().Format("20060102150405.000000000")+".aipdb")
	downloaded, err := client.Download(r.Context(), stream.ID, version.ID, tmpPath, maxImportBodyBytes)
	if err != nil {
		handleBackupServiceError(w, err)
		return
	}
	defer os.Remove(tmpPath)
	if downloaded.SizeBytes != version.SizeBytes || !strings.EqualFold(downloaded.SHA256, version.SHA256) {
		writeError(w, http.StatusBadGateway, "remote backup metadata changed while restoring; refresh versions and try again")
		return
	}
	s.installImportedDatabase(w, r, request.DatabaseName, request.DatabasePassword, copyBackupFile(tmpPath))
}

func findTransientRemoteBackup(r *http.Request, client *backups.ServiceClient, streamID, backupID string) (backups.ServiceStream, backups.ServiceBackup, error) {
	streams, err := client.ListStreams(r.Context())
	if err != nil {
		return backups.ServiceStream{}, backups.ServiceBackup{}, err
	}
	for _, stream := range streams {
		if stream.ID != strings.TrimSpace(streamID) {
			continue
		}
		versions, err := client.ListBackups(r.Context(), stream.ID)
		if err != nil {
			return backups.ServiceStream{}, backups.ServiceBackup{}, err
		}
		for _, version := range versions {
			if version.ID == strings.TrimSpace(backupID) {
				return stream, version, nil
			}
		}
		return backups.ServiceStream{}, backups.ServiceBackup{}, backups.ServiceError{StatusCode: http.StatusNotFound}
	}
	return backups.ServiceStream{}, backups.ServiceBackup{}, backups.ServiceError{StatusCode: http.StatusNotFound}
}
