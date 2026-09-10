package api

import (
	"errors"
	"net/http"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

type unlockRequest = gatewayinfra.UnlockRequest
type setupUnlockRequest = gatewayinfra.SetupRequest
type unlockStatusResponse = gatewayinfra.StatusResponse
type renameDatabaseRequest = gatewayinfra.RenameRequest
type deleteDatabaseRequest = gatewayinfra.DeleteRequest
type deleteLockedDatabaseRequest = gatewayinfra.DeleteLockedRequest
type switchDatabaseRequest = gatewayinfra.SwitchRequest
type changeDatabasePasswordRequest = gatewayinfra.ChangePasswordRequest

func (s *Server) workspaceLifecycleHTTPHandlers() *gatewayinfra.WorkspaceHTTPHandlers {
	return gatewayinfra.NewWorkspaceHTTP(gatewayinfra.WorkspaceHTTPDependencies{
		Lifecycle: s.workspaceState.Lifecycle,
		BeginAttempt: func(w http.ResponseWriter, r *http.Request) (gatewayinfra.PasswordAttempt, bool) {
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
	return gatewayinfra.ValidatePassword(password, confirmation)
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
	case errors.Is(err, gatewayinfra.ErrDatabaseInUse):
		writeError(w, http.StatusConflict, gatewayinfra.ErrDatabaseInUse.Error())
	case errors.Is(err, gatewayinfra.ErrAuthentication):
		writeError(w, http.StatusUnauthorized, "invalid unlock password or database")
	case gatewayinfra.UnsupportedSchemaMessage(err) != "":
		writeError(w, http.StatusConflict, gatewayinfra.UnsupportedSchemaMessage(err))
	case errors.Is(err, gatewayinfra.ErrInitialization):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "database runtime initialization failed")
	}
}

func recordDatabaseUnlockAttempt(attempt databasePasswordAttempt, err error) {
	if errors.Is(err, gatewayinfra.ErrAuthentication) {
		attempt.failure()
		return
	}
	attempt.success()
}
