package backup

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
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type ImportDatabaseRequest struct {
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

func (component *Component) downloadDatabase(w http.ResponseWriter, r *http.Request) {
	lease, err := component.acquireReadOperation(r.Context())
	if err != nil {
		httptransport.WriteError(w, http.StatusRequestTimeout, "database backup was canceled")
		return
	}
	defer lease.Release()
	if component.dependencies.HasSession == nil || !component.dependencies.HasSession(r) {
		httptransport.WriteError(w, http.StatusUnauthorized, "ui session required")
		return
	}
	runtime, ok := component.dependencies.ActiveRuntime(w)
	if !ok {
		return
	}
	snapshot, err := backups.CreateDatabaseSnapshot(r.Context(), backups.SnapshotSource{
		Database: runtime.Database, DatabaseID: runtime.DatabaseID, Path: runtime.DatabasePath,
	})
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	lease.ReleaseLifecycle()
	defer os.Remove(snapshot.Path)
	httpattachment.SetHeaders(w, snapshot.Filename, "application/octet-stream")
	http.ServeFile(w, r, snapshot.Path)
}

func (component *Component) importDatabase(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		httptransport.WriteError(w, http.StatusUnsupportedMediaType, "database import requires multipart/form-data")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, backups.MaxDatabaseTransferBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httptransport.WriteError(w, http.StatusRequestEntityTooLarge, "uploaded database is too large; maximum import size is 256 MiB")
			return
		}
		httptransport.WriteError(w, http.StatusBadRequest, "invalid multipart database upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	request := ImportDatabaseRequest{DatabaseName: strings.TrimSpace(r.FormValue("database_name")), DatabasePassword: r.FormValue("database_password")}
	defer clearStrings(&request.DatabasePassword)
	file, _, err := r.FormFile("sqlite")
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, "database file is required")
		return
	}
	defer file.Close()
	component.InstallImportedDatabase(w, r, request.DatabaseName, request.DatabasePassword, func(path string) error {
		output, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(output, file); err != nil {
			_ = output.Close()
			return err
		}
		return output.Close()
	}, nil)
}

func (component *Component) InstallImportedDatabase(w http.ResponseWriter, r *http.Request, databaseName, password string, writeTemp func(string) error, mutate func(*sql.DB) error) {
	attempt, ok := component.dependencies.BeginAttempt(w, r)
	if !ok {
		return
	}
	var prepared uisession.Prepared
	transition, err := component.dependencies.Lifecycle.Import(r.Context(), workspacelifecycle.ImportInput{
		DatabaseName: databaseName, Password: password, Write: writeTemp, Mutate: mutate,
		BeforePublish: func() error {
			var err error
			prepared, err = uisession.Prepare()
			return err
		},
	})
	if err != nil {
		if errors.Is(err, workspacelifecycle.ErrCredential) {
			attempt.Failure()
			httptransport.WriteError(w, http.StatusBadRequest, "invalid database password or database file")
			return
		}
		if workspacelifecycle.CredentialWasVerified(err) {
			attempt.Success()
		}
		writeImportError(w, err)
		return
	}
	attempt.Success()
	if component.dependencies.IssuePrepared == nil || component.dependencies.IssuePrepared(w, prepared) != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "imported", "state": "unlocked", "database_id": transition.Identity.ID,
		"imported_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func writeImportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspacelifecycle.ErrDatabaseExists), errors.Is(err, workspacelifecycle.ErrUnsupportedSchema):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, workspacelifecycle.ErrNameRequired), errors.Is(err, workspacelifecycle.ErrPasswordRequired),
		errors.Is(err, workspacelifecycle.ErrPlaintext), errors.Is(err, workspacelifecycle.ErrInvalidRequest):
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
	default:
		httptransport.WriteInternalError(w)
	}
}

func clearStrings(values ...*string) {
	for _, value := range values {
		if value != nil {
			*value = ""
		}
	}
}
