package api

import (
	"context"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) liveConsoleTargetRefForRuntimeID(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, runtimeID int64) (string, error) {
	return s.connectorRuntime.ResolveLiveConsoleTarget(ctx, runtime, s.connectorKinds(), runtimeID)
}

func (s *Server) connectorKinds() []string {
	kinds := make([]string, 0, len(s.connectorRegistry().List()))
	for _, info := range s.connectorRegistry().List() {
		kinds = append(kinds, info.Kind)
	}
	return kinds
}
