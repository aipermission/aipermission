package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
)

func (s *Server) connectorPortsApplication() *connectorapi.PortsComponent {
	return connectorapi.NewPorts(connectorapi.PortsDependencies{
		Peer: connectorapi.PeerDependencies{TrustStorePath: s.connectorTrustStorePath},
		Routes: connectorapi.RouteDependencies{
			ActiveRuntime: func(w http.ResponseWriter) bool {
				_, ok := s.activeRuntimeOrLocked(w)
				return ok
			},
			ChangePeerTrust: s.connectorChangeVaultPeerTrust,
		},
		LiveConsole: connectorapi.LiveConsoleDependencies{
			TransportAdapter: s.connectorLiveConsoleTransportAdapterFor,
			TargetAdapter:    s.connectorLiveConsoleTargetAdapterFor,
		},
	})
}

func connectorWorkspace(runtime databaseRuntime) connectorapi.Workspace {
	if runtime == nil {
		return connectorapi.Workspace{}
	}
	database := runtime.StoragePort().DatabaseHandle()
	return connectorapi.Workspace{
		Database: database,
		Connector: connectorapi.NewTransportRuntime(
			runtime.ConnectorPort(), database, runtime.SecurityPort().VaultDeliveryCoordinator().AcquireDelivery,
		),
		Principal: func() (gatewayaccess.Principal, error) {
			return localExecutionPrincipal(runtime)
		},
	}
}

func (s *Server) connectorPortsWorkspace(runtime databaseRuntime) connectorapi.Workspace {
	workspace := connectorWorkspace(runtime)
	if runtime == nil {
		return workspace
	}
	workspace.Actions = connectorapi.WorkspaceActionPorts{
		Restart: func(ctx context.Context, principal gatewayaccess.Principal, runtimeID int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
			result, err := s.restartServerConsoleSession(ctx, runtime, principal, runtimeID, runningError)
			return connectorapi.ConsoleRestartResult{ClosedSessionIDs: result.ClosedSessionIDs, CanceledRunningRequests: result.CanceledRunningRequests}, err
		},
		Finish: func(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText, errorText string, hints ...connectors.OutputHint) (connectormgmt.ActionRequest, error) {
			return s.finishConnectorActionRequest(ctx, runtime, requestID, status, output, displayText, errorText, hints...)
		},
	}
	workspace.Transfers = connectorapi.WorkspaceTransferPorts{
		RunDownloadBatch: func(ctx context.Context, authorization connectorapi.TransferAuthorization, runtimeID int64, paths []string, archiveName, source string) (connectorapi.TransferBatch, error) {
			if runtime.OperationsPort().FileTransferRuntime() == nil {
				return connectorapi.TransferBatch{}, errInvalidConnectorRuntime
			}
			batch, err := s.fileTransferHTTPHandlers().CreateAndLaunchDownloadBatch(ctx, runtime.OperationsPort().FileTransferRuntime(), authorization, runtimeID, paths, archiveName, source)
			return connectorapi.TransferBatch{ID: batch.ID, Status: batch.Status, ItemCount: len(batch.Items)}, err
		},
		RuntimeCapabilities: func(kind string) connectors.RuntimeCapabilityResolver {
			return connectorRuntimeCapabilitiesFor(kind, s, runtime)
		},
	}
	workspace.Targets = connectorapi.WorkspaceTargetPorts{
		Delete: func(ctx context.Context, target connectormgmt.Target, payload map[string]any) error {
			return s.connectorLifecycleApplication(runtime).DeleteTarget(ctx, target, payload)
		},
		Finalize: func(ctx context.Context, target connectormgmt.Target, reason string, payload map[string]any) (int64, error) {
			return s.connectorLifecycleApplication(runtime).FinalizeDeletedTarget(ctx, target, reason)
		},
		Audit: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
		},
	}
	return workspace
}

func connectorDataRuntimePort(runtime databaseRuntime, kind string) connectorapi.ConnectorDataRuntime {
	return connectorapi.DataRuntime(connectorWorkspace(runtime), kind)
}

func connectorLiveRuntime(runtime databaseRuntime, kind string) connectorapi.LiveConsoleRuntime {
	return connectorapi.LiveRuntime(connectorWorkspace(runtime), kind)
}

func connectorActionRuntime(runtime databaseRuntime, kind string) connectorapi.ActionRuntime {
	return connectorapi.PortActionRuntime(connectorWorkspace(runtime), kind)
}

func connectorCredentialResourceRuntime(runtime databaseRuntime, kind string) connectorapi.CredentialResourceRuntime {
	return connectorapi.PortCredentialResourceRuntime(connectorWorkspace(runtime), kind)
}

func (s *Server) connectorTargetLifecycleRuntime(runtime databaseRuntime, kind string) connectorapi.TargetLifecycleRuntime {
	return s.connectorPortsApplication().TargetLifecycleRuntime(connectorWorkspace(runtime), kind)
}

func connectorRuntimeIDBelongsToKind(ctx context.Context, runtime databaseRuntime, kind string, runtimeID int64) error {
	return connectorapi.RequireRuntimeID(ctx, connectorWorkspace(runtime), kind, runtimeID)
}

func connectorRuntimeIDBelongsToTarget(ctx context.Context, runtime databaseRuntime, kind string, targetID, runtimeID int64) error {
	return connectorapi.RequireTargetRuntimeID(ctx, connectorWorkspace(runtime), kind, targetID, runtimeID)
}
