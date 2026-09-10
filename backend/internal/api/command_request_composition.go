package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

func (s *Server) initializeCommandRequestRuntime(runtime *databaseRuntime) error {
	owner, err := gatewayaccess.NewCommandWorkspaceRuntime(gatewayaccess.CommandWorkspaceRuntimeDependencies{
		Database: runtime.Storage.Database, Vault: runtime.Storage.Vault, WorkspaceID: runtime.WorkspaceUUID,
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		Sessions: runtime.Connectors.ConsoleSessions, BackgroundTimeout: mcpBackgroundCommandTimeout,
	})
	if err != nil {
		return err
	}
	runtime.Operations.CommandRequests = owner
	return nil
}

func (s *Server) commandRequestHTTPScope(w http.ResponseWriter) (gatewayaccess.CommandHTTPReader, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	if runtime.Operations.CommandRequests == nil {
		writeInternalError(w)
		return nil, false
	}
	return runtime.Operations.CommandRequests, true
}
