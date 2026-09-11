package api

import (
	"context"
	"net/http"

	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (s *Server) initializeCommandRequestRuntime(runtime databaseRuntime) error {
	return s.commands.Initialize(runtime.Identity.RuntimeID, gatewayoperations.CommandRuntimeDependencies{
		Database: runtime.Storage.DatabaseHandle(), Vault: runtime.Storage.SecretVault(), WorkspaceID: runtime.Identity.WorkspaceID,
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		Sessions: runtime.Connectors.ConsoleSessionManager(), BackgroundTimeout: mcpBackgroundCommandTimeout,
	})
}

func (s *Server) commandRuntime(runtime databaseRuntime) (*gatewayoperations.CommandRuntime, error) {
	if runtime == nil {
		return nil, gatewayoperations.ErrCommandRuntimeUnavailable
	}
	return s.commands.Runtime(runtime.Identity.RuntimeID)
}

func (s *Server) commandRequestHTTPScope(w http.ResponseWriter) (*gatewayoperations.CommandRuntime, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	owner, err := s.commandRuntime(runtime)
	if err != nil {
		writeInternalError(w)
		return nil, false
	}
	return owner, true
}
