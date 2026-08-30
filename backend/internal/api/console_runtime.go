package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
)

func (s *Server) runtimeConsoleOpener(runtime *databaseRuntime) console.RuntimeOpener {
	return func(ctx context.Context, request console.RuntimeOpenRequest) (*console.RuntimeSession, error) {
		targetRef, err := liveConsoleTargetRefForRuntimeID(ctx, runtime, request.RuntimeID)
		if err != nil {
			return nil, err
		}
		target, _, err := connectortargets.NewStore(runtime.database).ResolveConnectorActionTarget(ctx, targetRef)
		if err != nil {
			return nil, err
		}
		adapter := s.connectorLiveConsoleTransportAdapterFor(target.ConnectorKind)
		if adapter == nil {
			return nil, connectortargets.ErrInvalidTargetRef
		}
		return adapter.OpenLiveConsole(ctx, s, runtime, request)
	}
}
