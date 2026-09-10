package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/backups"
)

type transientBackupRestoreRequest struct {
	BaseURL          string `json:"base_url"`
	Token            string `json:"token"`
	StreamID         string `json:"stream_id"`
	BackupID         string `json:"backup_id"`
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

func (s backupHandlers) restoreTransientRemoteBackup(w http.ResponseWriter, r *http.Request) {
	var request transientBackupRestoreRequest
	if !decodeJSON(w, r, &request) {
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
	prepared, err := backups.PrepareTransientRestore(r.Context(), s.config.DataPath, backups.TransientRestoreSelection{
		BaseURL: request.BaseURL, Token: request.Token, StreamID: request.StreamID, BackupID: request.BackupID,
	})
	if err != nil {
		backups.WriteServiceHTTPError(w, err)
		return
	}
	defer prepared.Remove()
	s.installImportedDatabaseWithMutator(w, r, request.DatabaseName, request.DatabasePassword, backups.CopyBackupFile(prepared.Path), func(database *sql.DB) error {
		return prepared.RecordBaseline(r.Context(), database)
	})
}
