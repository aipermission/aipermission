package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/commandrequests"
)

func (s *Server) initializeCommandRequestRuntime(runtime *databaseRuntime) error {
	owner, err := commandrequests.NewWorkspaceRuntime(commandrequests.WorkspaceRuntimeDependencies{
		Database: runtime.database, Vault: runtime.vault, WorkspaceID: runtime.workspaceUUID,
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		Sessions: runtime.consoleSessions, BackgroundTimeout: mcpBackgroundCommandTimeout,
	})
	if err != nil {
		return err
	}
	runtime.commandRequests = owner
	return nil
}

func (s *Server) commandRequestHTTPScope(w http.ResponseWriter) (commandrequests.HTTPReader, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	if runtime.commandRequests == nil {
		writeInternalError(w)
		return nil, false
	}
	return runtime.commandRequests, true
}
