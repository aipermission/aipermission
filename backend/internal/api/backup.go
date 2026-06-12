package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

const maxImportBodyBytes = 256 << 20

type importDatabaseRequest struct {
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

func (s backupHandlers) downloadDatabase(w http.ResponseWriter, r *http.Request) {
	s.lifecycleMu.RLock()
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		s.lifecycleMu.RUnlock()
		return
	}
	databaseID := runtime.id

	snapshotPath := filepath.Join(filepath.Dir(runtime.path), "."+databaseID+"-"+time.Now().UTC().Format("20060102150405")+".backup.aipdb")
	if err := dbpkg.Snapshot(runtime.database, snapshotPath); err != nil {
		s.lifecycleMu.RUnlock()
		writeInternalError(w)
		return
	}
	s.lifecycleMu.RUnlock()
	defer os.Remove(snapshotPath)

	filename := strings.Trim(databaseID, "-")
	if filename == "" {
		filename = "aipermission"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`-`+time.Now().UTC().Format("20060102-150405")+`.aipdb"`)
	http.ServeFile(w, r, snapshotPath)
}

func (s backupHandlers) importDatabase(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		s.importDatabaseMultipart(w, r)
		return
	}
	writeError(w, http.StatusUnsupportedMediaType, "database import requires multipart/form-data")
}

func (s backupHandlers) importDatabaseMultipart(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodyBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "uploaded database is too large; maximum import size is 256 MiB")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid multipart database upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	request := importDatabaseRequest{
		DatabaseName:     strings.TrimSpace(r.FormValue("database_name")),
		DatabasePassword: r.FormValue("database_password"),
	}
	defer clearStringReferences(&request.DatabasePassword)
	file, _, err := r.FormFile("sqlite")
	if err != nil {
		writeError(w, http.StatusBadRequest, "database file is required")
		return
	}
	defer file.Close()

	s.installImportedDatabase(w, r, request.DatabaseName, request.DatabasePassword, func(tmpPath string) error {
		output, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(output, file); err != nil {
			_ = output.Close()
			return err
		}
		return output.Close()
	})
}

func (s backupHandlers) installImportedDatabase(w http.ResponseWriter, r *http.Request, databaseName string, databasePassword string, writeTemp func(string) error) {
	databaseName = strings.TrimSpace(databaseName)
	if databaseName == "" {
		writeError(w, http.StatusBadRequest, "database name is required")
		return
	}
	if databasePassword == "" {
		writeError(w, http.StatusBadRequest, "database password is required")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	targetID, targetPath, err := dbpkg.NewDatabasePath(s.config.DataPath, databaseName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tmpPath := targetPath + ".import"
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		writeInternalError(w)
		return
	}
	if err := writeTemp(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		writeInternalError(w)
		return
	}

	if dbpkg.LooksLikePlainSQLite(tmpPath) {
		_ = os.Remove(tmpPath)
		writeError(w, http.StatusBadRequest, "plaintext SQLite imports are not supported; import an encrypted .aipdb database")
		return
	}
	testDB, err := dbpkg.OpenEncrypted(tmpPath, databasePassword)
	if err != nil {
		_ = os.Remove(tmpPath)
		writeError(w, http.StatusBadRequest, "invalid database password or database file")
		return
	}
	if _, err := gatewaySecretFromDatabase(testDB, s.config.GatewaySecret); err != nil {
		_ = testDB.Close()
		_ = os.Remove(tmpPath)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = testDB.Close()

	if dbpkg.Exists(targetPath) {
		_ = os.Remove(tmpPath)
		writeError(w, http.StatusConflict, "database name already exists")
		return
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		writeInternalError(w)
		return
	}

	s.activeDataPath = targetPath
	s.activeDatabase = targetID
	if err := s.openUnlockedLocked(databasePassword); err != nil {
		writeInternalError(w)
		return
	}
	if err := s.issueUISession(w); err != nil {
		writeInternalError(w)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "imported",
		"state":       "unlocked",
		"database_id": targetID,
		"imported_at": time.Now().UTC().Format(time.RFC3339),
	})
}
