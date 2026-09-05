package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

type connectorTargetLifecycleRuntimePort struct {
	connectorapi.LiveSessionRuntime
	runtime *databaseRuntime
}

func (p connectorTargetLifecycleRuntimePort) ConnectorLocalExecutionPrincipal() (executionprincipal.Principal, error) {
	if p.runtime == nil {
		return executionprincipal.Principal{}, errInvalidConnectorRuntime
	}
	return localExecutionPrincipal(p.runtime)
}

func connectorRuntimeScope(runtime *databaseRuntime, kind string) *connectorruntime.Scope {
	if runtime == nil {
		return connectorruntime.NewScope(kind, connectorruntime.Dependencies{})
	}
	return connectorruntime.NewScope(kind, connectorruntime.Dependencies{
		Database:        runtime.database,
		Vault:           runtime.vault,
		WorkspaceID:     runtime.workspaceUUID,
		Resources:       runtime.connectorResources,
		ConsoleSessions: runtime.consoleSessions,
		SecretAccessor: func(secrets map[string]any) connectors.SecretAccessor {
			return connectorSecretAccessor{values: secrets, boundary: newConnectorCredentialBoundary(secrets)}
		},
	})
}

func connectorDataRuntimePort(runtime *databaseRuntime, kind string) connectorapi.ConnectorDataRuntime {
	return connectorRuntimeScope(runtime, kind).DataRuntime()
}

func connectorLiveRuntime(runtime *databaseRuntime, kind string) connectorapi.LiveConsoleRuntime {
	return connectorRuntimeScope(runtime, kind).LiveConsoleRuntime()
}

func connectorActionRuntime(runtime *databaseRuntime, kind string) connectorapi.ActionRuntime {
	return connectorRuntimeScope(runtime, kind).ActionRuntime()
}

func connectorTransferRuntime(runtime *databaseRuntime, kind string) connectorapi.TransferRuntime {
	return connectorRuntimeScope(runtime, kind).TransferRuntime()
}

func connectorTargetLifecycleRuntime(runtime *databaseRuntime, kind string) connectorapi.TargetLifecycleRuntime {
	return connectorTargetLifecycleRuntimePort{LiveSessionRuntime: connectorRuntimeScope(runtime, kind).ActionRuntime(), runtime: runtime}
}

func connectorCredentialResourceRuntime(runtime *databaseRuntime, kind string) connectorapi.CredentialResourceRuntime {
	return connectorRuntimeScope(runtime, kind).DataRuntime()
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
	if p.server == nil || p.runtime == nil || p.runtime.database == nil {
		return nil, errInvalidConnectorRuntime
	}
	store := connectortargets.NewStore(p.runtime.database)
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

func (p connectorRuntimeActionGatewayPort) ConnectorCreateDownloadBatch(ctx context.Context, runtimeID int64, remotePaths []string, archiveName string, source string, status string) (filetransfer.BatchRecord, error) {
	if p.server == nil {
		return filetransfer.BatchRecord{}, errInvalidConnectorRuntime
	}
	if err := connectorRuntimeIDBelongsToKind(ctx, p.runtime, p.kind, runtimeID); err != nil {
		return filetransfer.BatchRecord{}, err
	}
	return fileTransferHandlers{p.server}.createDownloadBatch(ctx, p.runtime, runtimeID, remotePaths, archiveName, source, status)
}

func (p connectorRuntimeActionGatewayPort) ConnectorRunTransferBatch(batchID int64, overwrite bool) {
	if p.server == nil || p.runtime == nil || p.runtime.fileTransfers == nil {
		return
	}
	batch, err := p.runtime.fileTransfers.GetBatch(context.Background(), batchID)
	if err != nil || connectorRuntimeIDBelongsToKind(context.Background(), p.runtime, p.kind, batch.RuntimeID) != nil {
		return
	}
	fileTransferHandlers{p.server}.launchTransferBatch(p.runtime, batchID, overwrite)
}

type connectorActionFinishGatewayPort struct {
	server  *Server
	runtime *databaseRuntime
	kind    string
}

func (p connectorActionFinishGatewayPort) ConnectorFinishActionRequest(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	if p.server == nil || p.runtime == nil || p.runtime.database == nil {
		return connectortargets.ActionRequest{}, errInvalidConnectorRuntime
	}
	request, err := connectortargets.NewStore(p.runtime.database).GetActionRequest(ctx, requestID)
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
	return connectorRuntimeScope(runtime, kind).RequireRuntimeID(ctx, runtimeID)
}

func connectorRuntimeIDBelongsToTarget(ctx context.Context, runtime *databaseRuntime, kind string, targetID int64, runtimeID int64) error {
	return connectorRuntimeScope(runtime, kind).RequireTargetRuntimeID(ctx, targetID, runtimeID)
}

type connectorFileTransferPorts struct {
	gateway connectorapi.FileTransferGateway
	runtime connectorapi.TransferRuntime
}

func connectorFileTransferPortsForID(ctx context.Context, server *Server, runtime *databaseRuntime, runtimeID int64) connectorFileTransferPorts {
	kind := ""
	if runtime != nil && runtime.database != nil {
		if target, _, _, err := connectortargets.NewStore(runtime.database).TargetProfileByRuntimeID(ctx, runtimeID); err == nil {
			kind = target.ConnectorKind
		}
	}
	return connectorFileTransferPorts{
		gateway: connectorFileTransferGatewayPort{connectorPeerGatewayPort: connectorPeerGatewayPort{server: server}, runtime: runtime, kind: kind},
		runtime: connectorTransferRuntime(runtime, kind),
	}
}

var _ connectorapi.RouteGateway = connectorRouteGatewayPort{}
var _ connectorapi.LiveConsoleGateway = connectorLiveConsoleGatewayPort{}
var _ connectorapi.RuntimeActionGateway = connectorRuntimeActionGatewayPort{}
var _ connectorapi.ActionFinishGateway = connectorActionFinishGatewayPort{}
var _ connectorapi.FileTransferGateway = connectorFileTransferGatewayPort{}
var _ connectorapi.TargetDeletionGateway = connectorTargetDeletionGatewayPort{}
var _ connectorapi.TargetOperationGateway = connectorTargetOperationGatewayPort{}
