package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type connectorTargetLifecycleRuntimePort = connectortransport.TargetLifecycleRuntimePort

func connectorDataRuntimePort(runtime *databaseRuntime, kind string) connectorapi.ConnectorDataRuntime {
	return connectortransport.DataRuntime(runtime, kind)
}

func connectorLiveRuntime(runtime *databaseRuntime, kind string) connectorapi.LiveConsoleRuntime {
	return connectortransport.LiveRuntime(runtime, kind)
}

func connectorActionRuntime(runtime *databaseRuntime, kind string) connectorapi.ActionRuntime {
	return connectortransport.ActionRuntime(runtime, kind)
}

func connectorTargetLifecycleRuntime(runtime *databaseRuntime, kind string) connectorapi.TargetLifecycleRuntime {
	return connectortransport.TargetLifecycleRuntime(runtime, kind, func() (executionprincipal.Principal, error) {
		if runtime == nil {
			return executionprincipal.Principal{}, errInvalidConnectorRuntime
		}
		return localExecutionPrincipal(runtime)
	})
}

func connectorCredentialResourceRuntime(runtime *databaseRuntime, kind string) connectorapi.CredentialResourceRuntime {
	return connectortransport.CredentialResourceRuntime(runtime, kind)
}

var _ connectorapi.TargetLifecycleRuntime = connectorTargetLifecycleRuntimePort{}

type connectorPeerGatewayPort struct{ server *Server }

func (p connectorPeerGatewayPort) ConnectorTrustStorePath() string {
	if p.server == nil {
		return ""
	}
	return p.server.connectorTrustStorePath()
}

type connectorLiveConsoleGatewayPort struct {
	connectorPeerGatewayPort
	runtime *databaseRuntime
}

func (p connectorLiveConsoleGatewayPort) ConnectorOpenLiveConsole(ctx context.Context, targetRef string, rows int, cols int, params map[string]any) (*console.RuntimeSession, error) {
	if p.server == nil || p.runtime == nil || p.runtime.Storage.Database == nil {
		return nil, errInvalidConnectorRuntime
	}
	store := connectortargets.NewStore(p.runtime.Storage.Database)
	target, profile, err := store.ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return nil, err
	}
	adapter := p.server.connectorLiveConsoleTransportAdapterFor(target.ConnectorKind)
	targetAdapter := p.server.connectorLiveConsoleTargetAdapterFor(target.ConnectorKind)
	if adapter == nil || targetAdapter == nil {
		return nil, connectortargets.ErrInvalidTargetRef
	}
	surface, err := store.EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{ConnectorKind: target.ConnectorKind, TargetID: target.ID, ProfileID: profile.ID, CapabilityKind: targetAdapter.LiveConsoleCapabilityKind(), Label: profile.Label})
	if err != nil {
		return nil, err
	}
	return adapter.OpenLiveConsole(ctx, p, connectorLiveRuntime(p.runtime, target.ConnectorKind), console.RuntimeOpenRequest{RuntimeID: surface.ID, Rows: rows, Cols: cols, Params: params})
}

type connectorRouteGatewayPort struct{ connectorPeerGatewayPort }

func (p connectorRouteGatewayPort) ConnectorActiveRuntimeAvailable(w http.ResponseWriter) bool {
	if p.server == nil {
		return false
	}
	_, ok := p.server.activeRuntimeOrLocked(w)
	return ok
}

func (p connectorRouteGatewayPort) ConnectorChangeVaultPeerTrust(ctx context.Context, change func() error) error {
	if p.server == nil {
		return errInvalidConnectorRuntime
	}
	return p.server.connectorChangeVaultPeerTrust(ctx, change)
}

type connectorRuntimeActionGatewayPort struct {
	connectorPeerGatewayPort
	runtime *databaseRuntime
	kind    string
}

func (p connectorRuntimeActionGatewayPort) ConnectorRestartConsoleSession(ctx context.Context, principal executionprincipal.Principal, runtimeID int64, runningRequestError string) (connectorapi.ConsoleRestartResult, error) {
	if p.server == nil {
		return connectorapi.ConsoleRestartResult{}, errInvalidConnectorRuntime
	}
	if err := connectorRuntimeIDBelongsToKind(ctx, p.runtime, p.kind, runtimeID); err != nil {
		return connectorapi.ConsoleRestartResult{}, err
	}
	result, err := p.server.restartServerConsoleSession(ctx, p.runtime, principal, runtimeID, runningRequestError)
	if err != nil {
		return connectorapi.ConsoleRestartResult{}, err
	}
	return connectorapi.ConsoleRestartResult{ClosedSessionIDs: result.ClosedSessionIDs, CanceledRunningRequests: result.CanceledRunningRequests}, nil
}

func (p connectorRuntimeActionGatewayPort) ConnectorCreateAndRunDownloadBatch(ctx context.Context, authorization connectorapi.TransferAuthorization, runtimeID int64, remotePaths []string, archiveName string, source string) (connectorapi.TransferBatch, error) {
	if p.server == nil {
		return connectorapi.TransferBatch{}, errInvalidConnectorRuntime
	}
	if err := connectorRuntimeIDBelongsToKind(ctx, p.runtime, p.kind, runtimeID); err != nil {
		return connectorapi.TransferBatch{}, err
	}
	if p.runtime == nil || p.runtime.Operations.FileTransfers == nil {
		return connectorapi.TransferBatch{}, errInvalidConnectorRuntime
	}
	batch, err := p.server.fileTransferHTTPHandlers().CreateAndLaunchDownloadBatch(ctx, p.runtime.Operations.FileTransfers, authorization, runtimeID, remotePaths, archiveName, source)
	return connectorapi.TransferBatch{ID: batch.ID, Status: batch.Status, ItemCount: len(batch.Items)}, err
}

type connectorActionFinishGatewayPort struct {
	server  *Server
	runtime *databaseRuntime
	kind    string
}

func (p connectorActionFinishGatewayPort) ConnectorFinishActionRequest(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	if p.server == nil || p.runtime == nil || p.runtime.Storage.Database == nil {
		return connectortargets.ActionRequest{}, errInvalidConnectorRuntime
	}
	request, err := connectortargets.NewStore(p.runtime.Storage.Database).GetActionRequest(ctx, requestID)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if request.ConnectorKind != p.kind {
		return connectortargets.ActionRequest{}, connectortargets.ErrActionRequestNotFound
	}
	return p.server.finishConnectorActionRequest(ctx, p.runtime, requestID, status, output, displayText, errorText, hints...)
}

type connectorFileTransferGatewayPort struct {
	connectorPeerGatewayPort
	runtime *databaseRuntime
	kind    string
}

func (p connectorFileTransferGatewayPort) ConnectorRuntimeCapabilities() connectors.RuntimeCapabilityResolver {
	return connectorRuntimeCapabilitiesFor(p.kind, p.server, p.runtime)
}

type connectorTargetDeletionGatewayPort struct {
	connectorPeerGatewayPort
	handlers connectorTargetHandlers
	runtime  *databaseRuntime
	kind     string
	targetID int64
}

func (p connectorTargetDeletionGatewayPort) ConnectorRestartConsoleSession(ctx context.Context, principal executionprincipal.Principal, runtimeID int64, runningRequestError string) (connectorapi.ConsoleRestartResult, error) {
	return connectorRuntimeActionGatewayPort{connectorPeerGatewayPort: p.connectorPeerGatewayPort, runtime: p.runtime, kind: p.kind}.ConnectorRestartConsoleSession(ctx, principal, runtimeID, runningRequestError)
}

func (p connectorTargetDeletionGatewayPort) ConnectorDeleteTargetRecord(ctx context.Context, target connectortargets.Target, payload map[string]any) error {
	if target.ID != p.targetID || target.ConnectorKind != p.kind {
		return connectortargets.ErrTargetNotFound
	}
	return p.handlers.connectorDeleteTargetRecord(ctx, p.runtime, target, payload)
}

func (p connectorTargetDeletionGatewayPort) ConnectorFinalizeDeletedTarget(ctx context.Context, target connectortargets.Target, staleReason string, payload map[string]any) (int64, error) {
	if target.ID != p.targetID || target.ConnectorKind != p.kind {
		return 0, connectortargets.ErrTargetNotFound
	}
	return p.handlers.connectorFinalizeDeletedTarget(ctx, p.runtime, target, staleReason, payload)
}

type connectorTargetOperationGatewayPort struct {
	connectorPeerGatewayPort
	handlers connectorTargetHandlers
	runtime  *databaseRuntime
	kind     string
	targetID int64
}

func (p connectorTargetOperationGatewayPort) ConnectorWriteAudit(ctx context.Context, actorType string, tokenID *int64, runtimeID int64, action string, payload any) {
	if connectorRuntimeIDBelongsToTarget(ctx, p.runtime, p.kind, p.targetID, runtimeID) != nil {
		return
	}
	p.handlers.writeObservationAudit(ctx, p.runtime, actorType, tokenID, runtimeID, action, payload)
}

func newRuntimeActionPorts(server *Server, runtime *databaseRuntime, kind string) (connectorapi.RuntimeActionGateway, connectorapi.ActionRuntime) {
	return connectorRuntimeActionGatewayPort{connectorPeerGatewayPort: connectorPeerGatewayPort{server: server}, runtime: runtime, kind: kind}, connectorActionRuntime(runtime, kind)
}

func newActionFinishPorts(server *Server, runtime *databaseRuntime, kind string) (connectorapi.ActionFinishGateway, connectorapi.ActionRuntime) {
	return connectorActionFinishGatewayPort{server: server, runtime: runtime, kind: kind}, connectorActionRuntime(runtime, kind)
}

func connectorRuntimeIDBelongsToKind(ctx context.Context, runtime *databaseRuntime, kind string, runtimeID int64) error {
	return connectortransport.RequireRuntimeID(ctx, runtime, kind, runtimeID)
}

func connectorRuntimeIDBelongsToTarget(ctx context.Context, runtime *databaseRuntime, kind string, targetID int64, runtimeID int64) error {
	return connectortransport.RequireTargetRuntimeID(ctx, runtime, kind, targetID, runtimeID)
}

var _ connectorapi.RouteGateway = connectorRouteGatewayPort{}
var _ connectorapi.LiveConsoleGateway = connectorLiveConsoleGatewayPort{}
var _ connectorapi.RuntimeActionGateway = connectorRuntimeActionGatewayPort{}
var _ connectorapi.ActionFinishGateway = connectorActionFinishGatewayPort{}
var _ connectorapi.FileTransferGateway = connectorFileTransferGatewayPort{}
var _ connectorapi.TargetDeletionGateway = connectorTargetDeletionGatewayPort{}
var _ connectorapi.TargetOperationGateway = connectorTargetOperationGatewayPort{}
