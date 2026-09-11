package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

func (s *Server) initializeCommandRequestRuntime(runtime databaseRuntime) error {
	owner, err := gatewayaccess.NewCommandWorkspaceRuntime(gatewayaccess.CommandWorkspaceRuntimeDependencies{
		Database: runtime.StoragePort().DatabaseHandle(), Vault: runtime.StoragePort().SecretVault(), WorkspaceID: runtime.WorkspaceIdentifier(),
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		Sessions: runtime.ConnectorPort().ConsoleSessionManager(), BackgroundTimeout: mcpBackgroundCommandTimeout,
	})
	if err != nil {
		return err
	}
	runtime.OperationsPort().SetCommandRequestRuntime(owner)
	return nil
}

func (s *Server) commandRequestHTTPScope(w http.ResponseWriter) (gatewayaccess.CommandHTTPReader, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	if runtime.OperationsPort().CommandRequestRuntime() == nil {
		writeInternalError(w)
		return nil, false
	}
	return runtime.OperationsPort().CommandRequestRuntime(), true
}
