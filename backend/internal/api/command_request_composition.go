package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

func (s *Server) initializeCommandRequestRuntime(runtime databaseRuntime) error {
	return s.access.InitializeCommandRuntime(runtime.ComponentStatePort(), gatewayaccess.CommandRuntimeDependencies{
		Database: runtime.StoragePort().DatabaseHandle(), Vault: runtime.StoragePort().SecretVault(), WorkspaceID: runtime.WorkspaceIdentifier(),
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		Sessions: runtime.ConnectorPort().ConsoleSessionManager(), BackgroundTimeout: mcpBackgroundCommandTimeout,
	})
}

func (s *Server) commandRuntime(runtime databaseRuntime) (gatewayaccess.CommandRuntime, error) {
	if runtime == nil {
		return nil, gatewayaccess.ErrCommandRuntimeUnavailable
	}
	return s.access.CommandRuntime(runtime.ComponentStatePort())
}

func (s *Server) commandRequestHTTPScope(w http.ResponseWriter) (gatewayaccess.CommandHTTPReader, bool) {
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
