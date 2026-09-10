package api

import (
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	workspacehttp "github.com/aipermission/aipermission/backend/internal/workspacelifecycle/httpapi"
)

type unlockRequest = workspacehttp.UnlockRequest
type setupUnlockRequest = workspacehttp.SetupRequest
type unlockStatusResponse = workspacehttp.StatusResponse
type renameDatabaseRequest = workspacehttp.RenameRequest
type deleteDatabaseRequest = workspacehttp.DeleteRequest
type deleteLockedDatabaseRequest = workspacehttp.DeleteLockedRequest
type switchDatabaseRequest = workspacehttp.SwitchRequest
type changeDatabasePasswordRequest = workspacehttp.ChangePasswordRequest

func (s *Server) workspaceLifecycleHTTPHandlers() *workspacehttp.Handlers {
	return workspacehttp.New(workspacehttp.Dependencies{
		Lifecycle: s.workspaceState.Lifecycle,
		BeginAttempt: func(w http.ResponseWriter, r *http.Request) (workspacehttp.PasswordAttempt, bool) {
			return s.beginDatabasePasswordAttempt(w, r)
		},
		HasSession:       s.hasValidUISession,
		IssueSession:     s.issueUISessionLocked,
		ClearSessions:    s.clearUISessions,
		CloseMaintenance: func(reason string) { s.closeMaintenanceConsoleForLifecycle(reason) },
	})
}

func (s *Server) currentDatabaseNameLocked() string {
	status, err := s.workspaceState.Lifecycle.Status()
	if err == nil && status.DatabaseName != "" {
		return status.DatabaseName
	}
	return s.workspaceSelection().ID
}

func (attempt databasePasswordAttempt) Success() { attempt.success() }
func (attempt databasePasswordAttempt) Failure() { attempt.failure() }

func validateUnlockPassword(password, confirmation string) error {
	return workspacelifecycle.ValidatePassword(password, confirmation)
}

func clearStringReferences(values ...*string) {
	for _, value := range values {
		if value != nil {
			*value = ""
		}
	}
}

type errPasswordMismatch struct{}

func (errPasswordMismatch) Error() string { return "password confirmation does not match" }

func writeDatabaseUnlockError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrDatabaseInUse):
		writeError(w, http.StatusConflict, db.ErrDatabaseInUse.Error())
	case errors.Is(err, workspacelifecycle.ErrAuthentication):
		writeError(w, http.StatusUnauthorized, "invalid unlock password or database")
	case db.UnsupportedSchemaMessage(err) != "":
		writeError(w, http.StatusConflict, db.UnsupportedSchemaMessage(err))
	case errors.Is(err, workspacelifecycle.ErrInitialization):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "database runtime initialization failed")
	}
}

func recordDatabaseUnlockAttempt(attempt databasePasswordAttempt, err error) {
	if errors.Is(err, workspacelifecycle.ErrAuthentication) {
		attempt.failure()
		return
	}
	attempt.success()
}
