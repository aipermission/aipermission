package api

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
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

type databaseSnapshot struct {
	Path      string
	Filename  string
	CreatedAt time.Time
}

func (s backupHandlers) downloadDatabase(w http.ResponseWriter, r *http.Request) {
	s.lifecycleMu.RLock()
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		s.lifecycleMu.RUnlock()
		return
	}
	snapshot, err := createDatabaseSnapshot(runtime)
	if err != nil {
		s.lifecycleMu.RUnlock()
		writeInternalError(w)
		return
	}
	s.lifecycleMu.RUnlock()
	defer os.Remove(snapshot.Path)

	setAttachmentHeaders(w, snapshot.Filename, "application/octet-stream")
	http.ServeFile(w, r, snapshot.Path)
}

func createDatabaseSnapshot(runtime *databaseRuntime) (databaseSnapshot, error) {
	createdAt := time.Now().UTC()
	databaseID := runtime.id
	snapshotPath, err := reserveDatabaseTempPath(runtime.path, "snapshot-"+databaseID+"-*.aipdb")
	if err != nil {
		return databaseSnapshot{}, fmt.Errorf("reserve database snapshot path: %w", err)
	}
	if err := dbpkg.Snapshot(runtime.database, snapshotPath); err != nil {
		return databaseSnapshot{}, err
	}
	filename := strings.Trim(databaseID, "-")
	if filename == "" {
		filename = "aipermission"
	}
	return databaseSnapshot{
		Path:      snapshotPath,
		Filename:  filename + "-" + createdAt.Format("20060102-150405") + ".aipdb",
		CreatedAt: createdAt,
	}, nil
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
	s.installImportedDatabaseWithMutator(w, r, databaseName, databasePassword, writeTemp, nil)
}

func (s backupHandlers) installImportedDatabaseWithMutator(w http.ResponseWriter, r *http.Request, databaseName string, databasePassword string, writeTemp func(string) error, mutate func(*sql.DB) error) {
	databaseName = strings.TrimSpace(databaseName)
	if databaseName == "" {
		writeError(w, http.StatusBadRequest, "database name is required")
		return
	}
	if databasePassword == "" {
		writeError(w, http.StatusBadRequest, "database password is required")
		return
	}
	attempt, ok := s.beginDatabasePasswordAttempt(w, r)
	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	targetID, targetPath, err := dbpkg.NewDatabasePathExact(s.config.DataPath, databaseName)
	if err != nil {
		if errors.Is(err, dbpkg.ErrDatabaseExists) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Remove staging files left by builds that used the former fixed import path.
	if err := dbpkg.DeleteDatabase(targetPath + ".import"); err != nil {
		writeInternalError(w)
		return
	}
	tmpPath, err := reserveDatabaseTempPath(targetPath, "import-*.aipdb")
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		writeInternalError(w)
		return
	}
	if err := dbpkg.DeleteDatabase(tmpPath); err != nil {
		writeInternalError(w)
		return
	}
	defer cleanupImportCandidate(tmpPath)
	if err := writeTemp(tmpPath); err != nil {
		writeInternalError(w)
		return
	}

	if dbpkg.LooksLikePlainSQLite(tmpPath) {
		writeError(w, http.StatusBadRequest, "plaintext SQLite imports are not supported; import an encrypted .aipdb database")
		return
	}
	testDB, err := dbpkg.OpenEncryptedImportCandidate(tmpPath, databasePassword)
	if err != nil {
		if message := dbpkg.UnsupportedSchemaMessage(err); message != "" {
			attempt.success()
			writeError(w, http.StatusConflict, message)
			return
		}
		attempt.failure()
		writeError(w, http.StatusBadRequest, "invalid database password or database file")
		return
	}
	attempt.success()
	if _, err := gatewaySecretFromDatabase(testDB, s.config.GatewaySecret); err != nil {
		if closeErr := closeImportCandidate(testDB); closeErr != nil {
			writeInternalError(w)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if mutate != nil {
		if err := mutate(testDB); err != nil {
			if closeErr := closeImportCandidate(testDB); closeErr != nil {
				log.Printf("failed closing rejected import candidate path=%q error=%v", tmpPath, closeErr)
			}
			writeInternalError(w)
			return
		}
	}
	if err := closeImportCandidate(testDB); err != nil {
		writeInternalError(w)
		return
	}

	if dbpkg.Exists(targetPath) {
		writeError(w, http.StatusConflict, "database name already exists")
		return
	}
	preparedSession, err := prepareUISession()
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := s.publishDatabase(tmpPath, targetPath); err != nil {
		if dbpkg.Exists(targetPath) {
			if cleanupErr := rollbackImportedDatabase(targetPath); cleanupErr != nil {
				log.Printf("failed partially published database cleanup path=%q error=%v", targetPath, cleanupErr)
			}
		}
		writeInternalError(w)
		return
	}

	previousDataPath := s.activeDataPath
	previousDatabase := s.activeDatabase
	s.activeDataPath = targetPath
	s.activeDatabase = targetID
	if err := s.openUnlockedLocked(databasePassword); err != nil {
		s.activeDataPath = previousDataPath
		s.activeDatabase = previousDatabase
		if runtime := s.workspaces[previousDatabase]; runtime != nil {
			s.applyRuntimeLocked(runtime)
		}
		if cleanupErr := rollbackImportedDatabase(targetPath); cleanupErr != nil {
			log.Printf("failed imported database cleanup path=%q error=%v", targetPath, cleanupErr)
		}
		writeInternalError(w)
		return
	}
	s.issuePreparedUISession(w, preparedSession)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "imported",
		"state":       "unlocked",
		"database_id": targetID,
		"imported_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func closeImportCandidate(database *sql.DB) error {
	if _, err := database.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		_ = database.Close()
		return fmt.Errorf("checkpoint import candidate: %w", err)
	}
	if err := database.Close(); err != nil {
		return fmt.Errorf("close import candidate: %w", err)
	}
	return nil
}

func cleanupImportCandidate(path string) {
	if err := dbpkg.DeleteDatabase(path); err != nil {
		log.Printf("failed import candidate cleanup path=%q error=%v", path, err)
	}
}

func rollbackImportedDatabase(targetPath string) error {
	return dbpkg.DeleteDatabase(targetPath)
}
