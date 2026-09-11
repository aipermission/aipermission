package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

func (s *Server) initializeCommandRequestRuntime(runtime databaseRuntime) error {
	return s.access.InitializeCommandRuntime(runtime.Identity.RuntimeID, gatewayaccess.CommandRuntimeDependencies{
		Database: runtime.Storage.DatabaseHandle(), Vault: runtime.Storage.SecretVault(), WorkspaceID: runtime.Identity.WorkspaceID,
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		Sessions: runtime.Connectors.ConsoleSessionManager(), BackgroundTimeout: mcpBackgroundCommandTimeout,
	})
}

func (s *Server) commandRuntime(runtime databaseRuntime) (*gatewayaccess.CommandRuntime, error) {
	if runtime == nil {
		return nil, gatewayaccess.ErrCommandRuntimeUnavailable
	}
	return s.access.CommandRuntime(runtime.Identity.RuntimeID)
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
