package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
	gatewaytransfer "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
)

func (s *Server) connectorPortsApplication() *connectorports.PortsComponent {
	if s == nil || s.connectorPorts == nil {
		panic("connector ports component is not initialized")
	}
	return s.connectorPorts
}

func (s *Server) newConnectorPortsApplication() *connectorports.PortsComponent {
	return connectorports.NewPorts(connectorports.PortsDependencies{
		Peer: connectorports.PeerDependencies{TrustStorePath: s.connectorTrustStorePath},
		Routes: connectorports.RouteDependencies{
			ActiveRuntime: func(w http.ResponseWriter) bool {
				_, ok := s.activeRuntimeOrLocked(w)
				return ok
			},
			PeerTrust: s.connectorPeerTrustApplication(),
		},
		LiveConsole: connectorports.LiveConsoleDependencies{
			TransportAdapter: s.connectorLiveConsoleTransportAdapterFor,
			TargetAdapter:    s.connectorLiveConsoleTargetAdapterFor,
		},
	})
}

func (s *Server) connectorPeerTrustApplication() *connectorports.PeerTrustCoordinator {
	return connectorports.NewPeerTrustCoordinator(func() []connectorports.PeerTrustWorkspace {
		runtimes := s.unlockedRuntimeSnapshot()
		workspaces := make([]connectorports.PeerTrustWorkspace, 0, len(runtimes))
		for _, runtime := range runtimes {
			boundRuntime := runtime
			workspaces = append(workspaces, connectorports.PeerTrustWorkspace{
				Identifier:       runtime.DatabaseIdentifier(),
				AcquireExclusive: runtime.SecurityPort().VaultDeliveryCoordinator().AcquireExclusive,
				InvalidateAll: func(ctx context.Context, reason string) error {
					lifecycle, err := s.vaultSessionLifecycle(boundRuntime)
					if err != nil {
						return err
					}
					return lifecycle.InvalidateAll(ctx, reason)
				},
			})
		}
		return workspaces
	})
}

func (s *Server) connectorWorkspace(runtime databaseRuntime) connectorports.Workspace {
	workspace := connectorBaseWorkspace(runtime)
	if runtime != nil {
		workspace = workspace.WithPrincipal(func() (gatewayaccess.Principal, error) {
			return s.localExecutionPrincipal(runtime)
		})
	}
	return workspace
}

func connectorBaseWorkspace(runtime databaseRuntime) connectorports.Workspace {
	if runtime == nil {
		return connectorports.Workspace{}
	}
	database := runtime.StoragePort().DatabaseHandle()
	return connectorports.NewWorkspace(
		runtime.ConnectorPort(), database, runtime.SecurityPort().VaultDeliveryCoordinator().AcquireDelivery,
	)
}

func (s *Server) connectorPortsWorkspace(runtime databaseRuntime) connectorports.Workspace {
	workspace := s.connectorWorkspace(runtime)
	if runtime == nil {
		return workspace
	}
	workspace.Actions = connectorports.WorkspaceActionPorts{
		Restart: func(ctx context.Context, principal gatewayaccess.Principal, runtimeID int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
			result, err := s.restartServerConsoleSession(ctx, runtime, principal, runtimeID, runningError)
			return connectorapi.ConsoleRestartResult{ClosedSessionIDs: result.ClosedSessionIDs, CanceledRunningRequests: result.CanceledRunningRequests}, err
		},
		Finish: func(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText, errorText string, hints ...connectors.OutputHint) (connectormgmt.ActionRequest, error) {
			return s.finishConnectorActionRequest(ctx, runtime, requestID, status, output, displayText, errorText, hints...)
		},
	}
	workspace.Transfers = connectorports.WorkspaceTransferPorts{
		RunDownloadBatch: func(ctx context.Context, authorization connectorapi.TransferAuthorization, runtimeID int64, paths []string, archiveName, source string) (connectorapi.TransferBatch, error) {
			if !gatewaytransfer.FileTransferWorkspaceReady(runtime) {
				return connectorapi.TransferBatch{}, errInvalidConnectorRuntime
			}
			batch, err := s.fileTransferHTTPHandlers().CreateAndLaunchDownloadBatchForWorkspace(ctx, runtime, authorization, runtimeID, paths, archiveName, source)
			return connectorapi.TransferBatch{ID: batch.ID, Status: batch.Status, ItemCount: len(batch.Items)}, err
		},
		RuntimeCapabilities: func(kind string) connectors.RuntimeCapabilityResolver {
			return connectorRuntimeCapabilitiesFor(kind, s, runtime)
		},
	}
	workspace.Targets = connectorports.WorkspaceTargetPorts{
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

func (s *Server) connectorDataRuntimePort(runtime databaseRuntime, kind string) connectorapi.ConnectorDataRuntime {
	return connectorports.DataRuntime(s.connectorWorkspace(runtime), kind)
}

func (s *Server) connectorLiveRuntime(runtime databaseRuntime, kind string) connectorapi.LiveConsoleRuntime {
	return connectorports.LiveRuntime(s.connectorWorkspace(runtime), kind)
}

func (s *Server) connectorCredentialResourceRuntime(runtime databaseRuntime, kind string) connectorapi.CredentialResourceRuntime {
	return connectorports.PortCredentialResourceRuntime(s.connectorWorkspace(runtime), kind)
}

func (s *Server) connectorTargetLifecycleRuntime(runtime databaseRuntime, kind string) connectorapi.TargetLifecycleRuntime {
	return s.connectorPortsApplication().TargetLifecycleRuntime(s.connectorWorkspace(runtime), kind)
}
