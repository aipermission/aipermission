package api

import (
	"context"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
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
		session, err := s.connectorRuntime.OpenLiveConsole(
			ctx,
			runtime,
			target.ConnectorKind,
			connectorapi.LiveConsoleOpenRequest{
				RuntimeID: request.RuntimeID, Generation: request.Generation, Rows: request.Rows, Cols: request.Cols,
				Params: request.Params, HasEnvironment: request.HasEnvironment,
			},
		)
		if err != nil {
			return nil, err
		}
		var applyEnvironment func(context.Context, gatewayoperations.SessionEnvironment) error
		if session.ApplyEnvironment != nil {
			applyEnvironment = func(applyCtx context.Context, environment gatewayoperations.SessionEnvironment) error {
				return session.ApplyEnvironment(applyCtx, environment)
			}
		}
		return &gatewayoperations.RuntimeSession{
			Stdin: session.Stdin, Stdout: session.Stdout, Stderr: session.Stderr,
			Wait: session.Wait, Resize: session.Resize, Close: session.Close,
			ApplyEnvironment:         applyEnvironment,
			PeerIdentity:             session.PeerIdentity,
			StartupInputAfterConnect: session.StartupInputAfterConnect,
		}, nil
	}
}
