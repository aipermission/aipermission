package api

import (
	"context"
	"fmt"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s *Server) liveConsoleTargetRefForRuntimeID(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, runtimeID int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for _, info := range s.connectorRegistry().List() {
		adapter, _ := s.connectorAPIAdapterFor(info.Kind).(connectorapi.LiveConsoleTargetAdapter)
		if adapter == nil {
			continue
		}
		ref, err := adapter.LiveConsoleTargetRef(ctx, s.connectorLiveRuntime(runtime, info.Kind), runtimeID)
		if connectormgmt.IsRuntimeSurfaceNotFound(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("resolve %s live console runtime: %w", info.Kind, err)
		}
		if ref == "" {
			return "", fmt.Errorf("resolve %s live console runtime: empty target reference", info.Kind)
		}
		return ref, nil
	}
	return "", connectormgmt.InvalidTargetRefError()
}
