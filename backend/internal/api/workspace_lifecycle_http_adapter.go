package api

import (
	"net/http"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) workspaceLifecycleHTTPHandlers() gatewayinfra.WorkspaceHTTPHandlers {
	return s.workspaceOwner.WorkspaceHTTP(gatewayinfra.WorkspaceHTTPDependencies{
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
	name, err := s.workspaceOwner.WorkspaceDatabaseName()
	if err == nil && name != "" {
		return name
	}
	return s.workspaceSelection().ID
}

func (attempt databasePasswordAttempt) Success() { attempt.success() }
func (attempt databasePasswordAttempt) Failure() { attempt.failure() }
