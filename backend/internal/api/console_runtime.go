package api

import (
	"context"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (s *Server) runtimeConsoleOpener(runtime *databaseRuntime) gatewayoperations.RuntimeOpener {
	return func(ctx context.Context, request gatewayoperations.RuntimeOpenRequest) (*gatewayoperations.RuntimeSession, error) {
		targetRef, err := liveConsoleTargetRefForRuntimeID(ctx, runtime, request.RuntimeID)
		if err != nil {
			return nil, err
		}
		target, _, err := connectormgmt.NewStore(runtime.Storage.Database).ResolveConnectorActionTarget(ctx, targetRef)
		if err != nil {
			return nil, err
		}
		adapter := s.connectorLiveConsoleTransportAdapterFor(target.ConnectorKind)
		if adapter == nil {
			return nil, connectormgmt.ErrInvalidTargetRef
		}
		return adapter.OpenLiveConsole(ctx, s.connectorPortsApplication().LiveConsoleGateway(runtime), connectorLiveRuntime(runtime, target.ConnectorKind), request)
	}
}
