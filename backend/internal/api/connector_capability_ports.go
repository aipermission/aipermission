package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s *Server) connectorPortsApplication() *connectorapi.PortsComponent {
	if s == nil || s.connectorPorts == nil {
		panic("connector ports component is not initialized")
	}
	return s.connectorPorts
}

func (s *Server) newConnectorPortsApplication() *connectorapi.PortsComponent {
	return connectorapi.NewPorts(connectorapi.PortsDependencies{
		Peer: connectorapi.PeerDependencies{TrustStorePath: s.connectorTrustStorePath},
		Routes: connectorapi.RouteDependencies{
			ActiveRuntime: func(w http.ResponseWriter) bool {
				_, ok := s.activeRuntimeOrLocked(w)
				return ok
			},
			PeerTrust: s.connectorPeerTrustApplication(),
		},
		LiveConsole: connectorapi.LiveConsoleDependencies{
			TransportAdapter: s.connectorLiveConsoleTransportAdapterFor,
			TargetAdapter:    s.connectorLiveConsoleTargetAdapterFor,
		},
	})
}

func (s *Server) connectorPeerTrustApplication() *connectorapi.PeerTrustCoordinator {
	return connectorapi.NewPeerTrustCoordinator(func() []connectorapi.PeerTrustWorkspace {
		runtimes := s.unlockedRuntimeSnapshot()
		workspaces := make([]connectorapi.PeerTrustWorkspace, 0, len(runtimes))
		for _, runtime := range runtimes {
			boundRuntime := runtime
			workspaces = append(workspaces, connectorapi.PeerTrustWorkspace{
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

func (s *Server) connectorWorkspace(runtime databaseRuntime) connectorapi.Workspace {
	workspace := connectorBaseWorkspace(runtime)
	if runtime != nil {
		workspace = workspace.WithPrincipal(func() (gatewayaccess.Principal, error) {
			return s.localExecutionPrincipal(runtime)
		})
	}
	return workspace
}

func connectorBaseWorkspace(runtime databaseRuntime) connectorapi.Workspace {
	if runtime == nil {
		return connectorapi.Workspace{}
	}
	database := runtime.StoragePort().DatabaseHandle()
	return connectorapi.NewWorkspace(
		runtime.ConnectorPort(), database, runtime.SecurityPort().VaultDeliveryCoordinator().AcquireDelivery,
	)
}

func (s *Server) connectorPortsWorkspace(runtime databaseRuntime) connectorapi.Workspace {
	workspace := s.connectorWorkspace(runtime)
	if runtime == nil {
		return workspace
	}
	workspace.Actions = connectorapi.WorkspaceActionPorts{
		Restart: func(ctx context.Context, principal gatewayaccess.Principal, runtimeID int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
			result, err := s.restartServerConsoleSession(ctx, runtime, principal, runtimeID, runningError)
			return connectorapi.ConsoleRestartResult{ClosedSessionIDs: result.ClosedSessionIDs, CanceledRunningRequests: result.CanceledRunningRequests}, err
		},
		Finish: func(ctx context.Context, requestID int64, status connectorapi.ResultStatus, output any, displayText, errorText string, hints ...connectorapi.OutputHint) (connectormgmt.ActionRequest, error) {
			return s.finishConnectorActionRequest(ctx, runtime, requestID, status, output, displayText, errorText, hints...)
		},
	}
	workspace.Transfers = connectorapi.WorkspaceTransferPorts{
		RunDownloadBatch: func(ctx context.Context, authorization connectorapi.TransferAuthorization, runtimeID int64, paths []string, archiveName, source string) (connectorapi.TransferBatch, error) {
			if runtime.OperationsPort().FileTransferRuntime() == nil {
				return connectorapi.TransferBatch{}, errInvalidConnectorRuntime
			}
			batch, err := s.fileTransferHTTPHandlers().CreateAndLaunchDownloadBatchForWorkspace(ctx, runtime.OperationsPort(), authorization, runtimeID, paths, archiveName, source)
			return connectorapi.TransferBatch{ID: batch.ID, Status: batch.Status, ItemCount: len(batch.Items)}, err
		},
		RuntimeCapabilities: func(kind string) connectorapi.RuntimeCapabilityResolver {
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

func (s *Server) connectorDataRuntimePort(runtime databaseRuntime, kind string) connectorapi.ConnectorDataRuntime {
	return connectorapi.DataRuntime(s.connectorWorkspace(runtime), kind)
}

func (s *Server) connectorLiveRuntime(runtime databaseRuntime, kind string) connectorapi.LiveConsoleRuntime {
	return connectorapi.LiveRuntime(s.connectorWorkspace(runtime), kind)
}

func (s *Server) connectorCredentialResourceRuntime(runtime databaseRuntime, kind string) connectorapi.CredentialResourceRuntime {
	return connectorapi.PortCredentialResourceRuntime(s.connectorWorkspace(runtime), kind)
}

func (s *Server) connectorTargetLifecycleRuntime(runtime databaseRuntime, kind string) connectorapi.TargetLifecycleRuntime {
	return s.connectorPortsApplication().TargetLifecycleRuntime(s.connectorWorkspace(runtime), kind)
}
