package api

import (
	"context"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	"net/http"

	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (s *Server) initializeCommandRequestRuntime(runtime *gatewayinfra.WorkspaceHandle) error {
	return s.operationsOwner.InitializeCommandRuntime(runtime, &s.commands, func(ctx context.Context, value string) string {
		return s.redactForPersistence(ctx, runtime, value)
	}, mcpBackgroundCommandTimeout)
}

func (s *Server) commandRuntime(runtime *gatewayinfra.WorkspaceHandle) (*gatewayoperations.CommandRuntime, error) {
	if runtime == nil {
		return nil, gatewayoperations.ErrCommandRuntimeUnavailable
	}
	return s.commands.Runtime(runtime.Identity().RuntimeID)
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
