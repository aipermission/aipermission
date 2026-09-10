package backups

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type TransientServiceRequest struct {
	BaseURL  string `json:"base_url"`
	Token    string `json:"token"`
	StreamID string `json:"stream_id,omitempty"`
}

type TransientRestoreSelection struct {
	BaseURL  string
	Token    string
	StreamID string
	BackupID string
}

var (
	ErrTransientBackupTooLarge = errors.New("remote backup is too large to restore through the gateway")
	ErrTransientBackupChanged  = errors.New("remote backup metadata changed while restoring; refresh versions and try again")
)

type TransientStreamResponse struct {
	ID           string          `json:"id"`
	DatabaseName string          `json:"database_name"`
	Backups      []ServiceBackup `json:"backups"`
}

type TransientHTTPHandlers struct{}

func NewTransientHTTPHandlers() *TransientHTTPHandlers { return &TransientHTTPHandlers{} }

func (h *TransientHTTPHandlers) List(w http.ResponseWriter, r *http.Request) {
	var request TransientServiceRequest
	if !httptransport.DecodeJSON(w, r, &request, 0) {
		return
	}
	defer func() { request.Token = "" }()
	client, err := NewServiceClient(request.BaseURL, request.Token)
	if err != nil {
		WriteServiceHTTPError(w, err)
		return
	}
	if _, err := client.Info(r.Context()); err != nil {
		WriteServiceHTTPError(w, err)
		return
	}
	streams, err := client.ListStreams(r.Context())
	if err != nil {
		WriteServiceHTTPError(w, err)
		return
	}
	if len(streams) > 100 {
		httptransport.WriteError(w, http.StatusBadGateway, "backup service returned too many streams for first-run restore")
		return
	}
	streamID := strings.TrimSpace(request.StreamID)
	items := make([]TransientStreamResponse, 0, len(streams))
	for _, stream := range streams {
		if streamID != "" && stream.ID != streamID {
			continue
		}
		var versions []ServiceBackup
		if streamID != "" {
			versions, err = client.ListBackups(r.Context(), stream.ID)
			if err != nil {
				WriteServiceHTTPError(w, err)
				return
			}
		}
		items = append(items, TransientStreamResponse{ID: stream.ID, DatabaseName: stream.DatabaseName, Backups: versions})
	}
	if streamID != "" && len(items) == 0 {
		httptransport.WriteError(w, http.StatusNotFound, "remote backup stream was not found")
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

type PreparedTransientRestore struct {
	Path     string
	BaseURL  string
	StreamID string
	Version  ServiceBackup
}

func (prepared PreparedTransientRestore) Remove() { _ = os.Remove(prepared.Path) }

func (prepared PreparedTransientRestore) RecordBaseline(ctx context.Context, database *sql.DB) error {
	return WriteServiceBaseline(ctx, database, prepared.BaseURL, prepared.StreamID, prepared.Version)
}

func PrepareTransientRestore(ctx context.Context, installationDataPath string, request TransientRestoreSelection) (PreparedTransientRestore, error) {
	if strings.TrimSpace(installationDataPath) == "" {
		return PreparedTransientRestore{}, ErrIncompleteScope
	}
	client, err := NewServiceClient(request.BaseURL, request.Token)
	if err != nil {
		return PreparedTransientRestore{}, err
	}
	if _, err := client.Info(ctx); err != nil {
		return PreparedTransientRestore{}, err
	}
	stream, version, err := findTransientRemoteBackup(ctx, client, request.StreamID, request.BackupID)
	if err != nil {
		return PreparedTransientRestore{}, err
	}
	if version.SizeBytes > MaxDatabaseTransferBytes {
		return PreparedTransientRestore{}, ErrTransientBackupTooLarge
	}
	tmpPath, err := databasecatalog.ReserveTempPath(installationDataPath, "first-run-restore-*.aipdb")
	if err != nil {
		return PreparedTransientRestore{}, err
	}
	downloaded, err := client.Download(ctx, stream.ID, version.ID, tmpPath, MaxDatabaseTransferBytes)
	if err != nil {
		_ = os.Remove(tmpPath)
		return PreparedTransientRestore{}, err
	}
	if downloaded.SizeBytes != version.SizeBytes || !strings.EqualFold(downloaded.SHA256, version.SHA256) {
		_ = os.Remove(tmpPath)
		return PreparedTransientRestore{}, ErrTransientBackupChanged
	}
	return PreparedTransientRestore{
		Path: tmpPath, BaseURL: request.BaseURL, StreamID: stream.ID, Version: version,
	}, nil
}

func findTransientRemoteBackup(ctx context.Context, client *ServiceClient, streamID, backupID string) (ServiceStream, ServiceBackup, error) {
	streams, err := client.ListStreams(ctx)
	if err != nil {
		return ServiceStream{}, ServiceBackup{}, err
	}
	streamID = strings.TrimSpace(streamID)
	backupID = strings.TrimSpace(backupID)
	for _, stream := range streams {
		if stream.ID != streamID {
			continue
		}
		versions, err := client.ListBackups(ctx, stream.ID)
		if err != nil {
			return ServiceStream{}, ServiceBackup{}, err
		}
		for _, version := range versions {
			if version.ID == backupID {
				return stream, version, nil
			}
		}
		return ServiceStream{}, ServiceBackup{}, ServiceError{StatusCode: http.StatusNotFound}
	}
	return ServiceStream{}, ServiceBackup{}, ServiceError{StatusCode: http.StatusNotFound}
}
