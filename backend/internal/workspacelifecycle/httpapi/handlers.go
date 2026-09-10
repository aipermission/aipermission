package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type Lifecycle interface {
	IsUnlocked() bool
	Status() (workspacelifecycle.Status, error)
	Setup(string, string, string) (workspacelifecycle.Transition, error)
	Unlock(string, string) (workspacelifecycle.Transition, error)
	Lock(string) (workspacelifecycle.Status, error)
	WillLockAll(string) bool
	Rename(context.Context, string, string) (workspacelifecycle.Transition, error)
	DeleteCurrent(context.Context, string, string) (workspacelifecycle.Transition, error)
	DeleteLocked(string, string) (workspacelifecycle.Transition, error)
	Switch(string, string) (workspacelifecycle.Transition, error)
	ChangePassword(context.Context, string, string) error
}

type PasswordAttempt interface {
	Success()
	Failure()
}

type Dependencies struct {
	Lifecycle        Lifecycle
	BeginAttempt     func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool)
	HasSession       func(*http.Request) bool
	IssueSession     func(http.ResponseWriter) error
	ClearSessions    func(http.ResponseWriter)
	CloseMaintenance func(string)
	Now              func() time.Time
}

type Handlers struct{ dependencies Dependencies }

func New(dependencies Dependencies) *Handlers { return &Handlers{dependencies: dependencies} }

func (h *Handlers) beginAttempt(w http.ResponseWriter, r *http.Request) (PasswordAttempt, bool) {
	if h.dependencies.BeginAttempt == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	return h.dependencies.BeginAttempt(w, r)
}

func (h *Handlers) now() time.Time {
	if h.dependencies.Now != nil {
		return h.dependencies.Now()
	}
	return time.Now()
}

func clearStrings(values ...*string) {
	for _, value := range values {
		if value != nil {
			*value = ""
		}
	}
}

func writeUnlockError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrDatabaseInUse):
		httptransport.WriteError(w, http.StatusConflict, db.ErrDatabaseInUse.Error())
	case errors.Is(err, workspacelifecycle.ErrCredential), errors.Is(err, workspacelifecycle.ErrAuthentication):
		httptransport.WriteError(w, http.StatusUnauthorized, "invalid unlock password or database")
	case db.UnsupportedSchemaMessage(err) != "":
		httptransport.WriteError(w, http.StatusConflict, db.UnsupportedSchemaMessage(err))
	case errors.Is(err, workspacelifecycle.ErrInitialization):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, workspacelifecycle.ErrLocked):
		httptransport.WriteError(w, http.StatusLocked, err.Error())
	case errors.Is(err, workspacelifecycle.ErrNotInitialized):
		httptransport.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, workspacelifecycle.ErrPasswordRequired):
		httptransport.WriteError(w, http.StatusBadRequest, "password is required")
	case errors.Is(err, workspacelifecycle.ErrPlaintext):
		httptransport.WriteError(w, http.StatusConflict, "plaintext SQLite databases are not supported; create or import an encrypted .aipdb database")
	default:
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
	}
}

type UnlockRequest struct {
	Password   string `json:"password"`
	DatabaseID string `json:"database_id"`
}

type SetupRequest struct {
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	DatabaseID      string `json:"database_id"`
	DatabaseName    string `json:"database_name"`
}

type RenameRequest struct {
	DatabaseName    string `json:"database_name"`
	CurrentPassword string `json:"current_password"`
}

type DeleteRequest struct {
	ConfirmName     string `json:"confirm_name"`
	CurrentPassword string `json:"current_password"`
}

type DeleteLockedRequest struct {
	DatabaseID      string `json:"database_id"`
	CurrentPassword string `json:"current_password"`
}

type SwitchRequest struct {
	DatabaseID string `json:"database_id"`
	Password   string `json:"password"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

type StatusResponse struct {
	State                  string                         `json:"state"`
	DataPath               string                         `json:"data_path,omitempty"`
	DatabaseID             string                         `json:"database_id"`
	DatabaseName           string                         `json:"database_name"`
	DatabaseSizeBytes      int64                          `json:"database_size_bytes,omitempty"`
	UISessionAuthenticated bool                           `json:"ui_session_authenticated"`
	Databases              []databasecatalog.DatabaseInfo `json:"databases"`
}
