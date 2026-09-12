package api

import (
	"context"
	"errors"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (s *Server) runtimeConsoleOpener(runtime *gatewayinfra.WorkspaceHandle) gatewayoperations.RuntimeOpener {
	return func(ctx context.Context, request gatewayoperations.RuntimeOpenRequest) (*gatewayoperations.RuntimeSession, error) {
		targetRef, err := s.liveConsoleTargetRefForRuntimeID(ctx, runtime, request.RuntimeID)
		if err != nil {
			return nil, err
		}
		target, _, err := s.connectorCatalog(runtime).ResolveActionTarget(ctx, targetRef)
		if err != nil {
			return nil, err
		}
		adapter := s.connectorLiveConsoleTransportAdapterFor(target.ConnectorKind)
		if adapter == nil {
			return nil, connectormgmt.InvalidTargetRefError()
		}
		session, err := adapter.OpenLiveConsole(
			ctx,
			s.connectorPortsApplication().LiveConsoleGateway(s.connectorPortsWorkspace(runtime)),
			s.connectorLiveRuntime(runtime, target.ConnectorKind),
			connectorapi.LiveConsoleOpenRequest{
				RuntimeID: request.RuntimeID, Generation: request.Generation, Rows: request.Rows, Cols: request.Cols,
				Params: request.Params, HasEnvironment: request.HasEnvironment,
			},
		)
		if err != nil {
			return nil, err
		}
		if session == nil {
			return nil, errors.New("connector live console returned no session")
		}
		return &gatewayoperations.RuntimeSession{
			Stdin: session.Stdin, Stdout: session.Stdout, Stderr: session.Stderr,
			Wait: session.Wait, Resize: session.Resize, Close: session.Close,
			ApplyEnvironment: session.ApplyEnvironment, PeerIdentity: session.PeerIdentity,
			StartupInputAfterConnect: session.StartupInputAfterConnect,
		}, nil
	}
}
