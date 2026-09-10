package api

import (
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

type unlockRequest = gatewayworkspace.UnlockRequest
type setupUnlockRequest = gatewayworkspace.SetupRequest
type unlockStatusResponse = gatewayworkspace.StatusResponse
type renameDatabaseRequest = gatewayworkspace.RenameRequest
type deleteDatabaseRequest = gatewayworkspace.DeleteRequest
type deleteLockedDatabaseRequest = gatewayworkspace.DeleteLockedRequest
type switchDatabaseRequest = gatewayworkspace.SwitchRequest
type changeDatabasePasswordRequest = gatewayworkspace.ChangePasswordRequest

func (s *Server) workspaceLifecycleHTTPHandlers() *gatewayworkspace.HTTPHandlers {
	return gatewayworkspace.NewHTTP(gatewayworkspace.HTTPDependencies{
		Lifecycle: s.workspaceState.Lifecycle,
		BeginAttempt: func(w http.ResponseWriter, r *http.Request) (gatewayworkspace.PasswordAttempt, bool) {
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
	return gatewayworkspace.ValidatePassword(password, confirmation)
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
	case errors.Is(err, gatewayworkspace.ErrDatabaseInUse):
		writeError(w, http.StatusConflict, gatewayworkspace.ErrDatabaseInUse.Error())
	case errors.Is(err, gatewayworkspace.ErrAuthentication):
		writeError(w, http.StatusUnauthorized, "invalid unlock password or database")
	case gatewayworkspace.UnsupportedSchemaMessage(err) != "":
		writeError(w, http.StatusConflict, gatewayworkspace.UnsupportedSchemaMessage(err))
	case errors.Is(err, gatewayworkspace.ErrInitialization):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "database runtime initialization failed")
	}
}

func recordDatabaseUnlockAttempt(attempt databasePasswordAttempt, err error) {
	if errors.Is(err, gatewayworkspace.ErrAuthentication) {
		attempt.failure()
		return
	}
	attempt.success()
}
