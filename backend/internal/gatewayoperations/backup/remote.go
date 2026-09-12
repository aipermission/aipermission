package backup

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type TransientRestoreRequest struct {
	BaseURL          string `json:"base_url"`
	Token            string `json:"token"`
	StreamID         string `json:"stream_id"`
	BackupID         string `json:"backup_id"`
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

func (component *Component) restoreTransientRemoteBackup(w http.ResponseWriter, r *http.Request) {
	var request TransientRestoreRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	defer clearStrings(&request.Token, &request.DatabasePassword)
	if strings.TrimSpace(request.DatabasePassword) == "" {
		httptransport.WriteError(w, http.StatusBadRequest, "database password is required")
		return
	}
	if strings.TrimSpace(request.DatabaseName) == "" {
		httptransport.WriteError(w, http.StatusBadRequest, "database name is required")
		return
	}
	prepared, err := backups.PrepareTransientRestore(r.Context(), component.dependencies.DataPath, backups.TransientRestoreSelection{
		BaseURL: request.BaseURL, Token: request.Token, StreamID: request.StreamID, BackupID: request.BackupID,
	})
	if err != nil {
		backups.WriteServiceHTTPError(w, err)
		return
	}
	defer prepared.Remove()
	component.installImportedDatabase(w, r, request.DatabaseName, request.DatabasePassword, backups.CopyBackupFile(prepared.Path), func(database *sql.DB) error {
		return prepared.RecordBaseline(r.Context(), database)
	})
}

func parsePositivePathID(w http.ResponseWriter, r *http.Request, key, label string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue(key)), 10, 64)
	if err != nil || id < 1 {
		httptransport.WriteError(w, http.StatusBadRequest, label+" is required")
		return 0, false
	}
	return id, true
}
