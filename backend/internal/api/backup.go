package api

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/httpattachment"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type importDatabaseRequest struct {
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

func (s backupHandlers) downloadDatabase(w http.ResponseWriter, r *http.Request) {
	releaseBackup, err := s.controlState.BackupOperations.Acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "database backup was canceled")
		return
	}
	defer releaseBackup()
	releaseLifecycle := s.workspaceState.Lifecycle.AcquireRead()
	// This route releases the lifecycle lock before streaming the completed
	// snapshot, so repeat the database-bound session check after acquiring it.
	if !s.hasValidUISession(r) {
		releaseLifecycle()
		writeError(w, http.StatusUnauthorized, "ui session required")
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		releaseLifecycle()
		return
	}
	snapshot, err := backups.CreateDatabaseSnapshot(r.Context(), backups.SnapshotSource{
		Database: runtime.Storage.Database, DatabaseID: runtime.ID, Path: runtime.Path,
	})
	if err != nil {
		releaseLifecycle()
		writeInternalError(w)
		return
	}
	releaseLifecycle()
	defer os.Remove(snapshot.Path)

	httpattachment.SetHeaders(w, snapshot.Filename, "application/octet-stream")
	http.ServeFile(w, r, snapshot.Path)
}

func (s backupHandlers) importDatabase(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		s.importDatabaseMultipart(w, r)
		return
	}
	writeError(w, http.StatusUnsupportedMediaType, "database import requires multipart/form-data")
}

func (s backupHandlers) importDatabaseMultipart(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, backups.MaxDatabaseTransferBytes)
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
	attempt, ok := s.beginDatabasePasswordAttempt(w, r)
	if !ok {
		return
	}
	var preparedSession preparedUISession
	transition, err := s.workspaceState.Lifecycle.Import(r.Context(), workspacelifecycle.ImportInput{
		DatabaseName: databaseName, Password: databasePassword, Write: writeTemp, Mutate: mutate,
		BeforePublish: func() error {
			var err error
			preparedSession, err = prepareUISession()
			return err
		},
	})
	if err != nil {
		if errors.Is(err, workspacelifecycle.ErrCredential) {
			attempt.failure()
			writeError(w, http.StatusBadRequest, "invalid database password or database file")
			return
		}
		if workspacelifecycle.CredentialWasVerified(err) {
			attempt.success()
		}
		writeWorkspaceImportError(w, err)
		return
	}
	attempt.success()
	if err := s.issuePreparedUISessionLocked(w, preparedSession); err != nil {
		writeInternalError(w)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "imported",
		"state":       "unlocked",
		"database_id": transition.Identity.ID,
		"imported_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func writeWorkspaceImportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspacelifecycle.ErrDatabaseExists), errors.Is(err, workspacelifecycle.ErrUnsupportedSchema):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, workspacelifecycle.ErrNameRequired), errors.Is(err, workspacelifecycle.ErrPasswordRequired),
		errors.Is(err, workspacelifecycle.ErrPlaintext), errors.Is(err, workspacelifecycle.ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeInternalError(w)
	}
}
