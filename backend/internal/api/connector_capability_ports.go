package api

import (
	"context"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
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
			workspace, ok := s.infrastructure.PeerTrustWorkspace(runtime,
				func(ctx context.Context, reason string) error {
					lifecycle, err := s.vaultSessionLifecycle(boundRuntime)
					if err != nil {
						return err
					}
					return lifecycle.InvalidateAll(ctx, reason)
				})
			if ok {
				workspaces = append(workspaces, workspace)
			}
		}
		return workspaces
	})
}

func (s *Server) connectorWorkspace(runtime *gatewayinfra.WorkspaceHandle) connectorports.Workspace {
	if runtime == nil {
		return connectorports.Workspace{}
	}
	workspace, _ := s.infrastructure.ConnectorPortsWorkspace(runtime, connectorports.Workspace{
		Principal: func() (gatewayaccess.Principal, error) { return s.localExecutionPrincipal(runtime) },
	})
	return workspace
}

func (s *Server) connectorPortsWorkspace(runtime *gatewayinfra.WorkspaceHandle) connectorports.Workspace {
	if runtime == nil {
		return connectorports.Workspace{}
	}
	ports := connectorports.Workspace{Actions: connectorports.WorkspaceActionPorts{
		Restart: func(ctx context.Context, principal gatewayaccess.Principal, runtimeID int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
			result, err := s.restartServerConsoleSession(ctx, runtime, principal, runtimeID, runningError)
			return connectorapi.ConsoleRestartResult{ClosedSessionIDs: result.ClosedSessionIDs, CanceledRunningRequests: result.CanceledRunningRequests}, err
		},
		Finish: func(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText, errorText string, hints ...connectors.OutputHint) (connectormgmt.ActionRequest, error) {
			return s.finishConnectorActionRequest(ctx, runtime, requestID, status, output, displayText, errorText, hints...)
		},
	}, Transfers: connectorports.WorkspaceTransferPorts{
		RunDownloadBatch: func(ctx context.Context, authorization connectorapi.TransferAuthorization, runtimeID int64, paths []string, archiveName, source string) (connectorapi.TransferBatch, error) {
			workspace := fileTransferWorkspaceIdentity(runtime)
			if !s.transfers.WorkspaceReady(workspace) {
				return connectorapi.TransferBatch{}, errInvalidConnectorRuntime
			}
			batch, err := s.transfers.CreateAndLaunchDownloadBatch(ctx, workspace, s.fileTransferHTTPHandlers(), authorization, runtimeID, paths, archiveName, source)
			return connectorapi.TransferBatch{ID: batch.ID, Status: batch.Status, ItemCount: len(batch.Items)}, err
		},
		RuntimeCapabilities: func(kind string) connectors.RuntimeCapabilityResolver {
			return connectorRuntimeCapabilitiesFor(kind, s, runtime)
		},
	}, Targets: connectorports.WorkspaceTargetPorts{
		Delete: func(ctx context.Context, target connectormgmt.Target, payload map[string]any) error {
			return s.connectorLifecycleApplication(runtime).DeleteTarget(ctx, target, payload)
		},
		Finalize: func(ctx context.Context, target connectormgmt.Target, reason string, payload map[string]any) (int64, error) {
			return s.connectorLifecycleApplication(runtime).FinalizeDeletedTarget(ctx, target, reason)
		},
		Audit: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
		},
	}}
	ports.Principal = func() (gatewayaccess.Principal, error) { return s.localExecutionPrincipal(runtime) }
	workspace, _ := s.infrastructure.ConnectorPortsWorkspace(runtime, ports)
	return workspace
}

func (s *Server) connectorDataRuntimePort(runtime *gatewayinfra.WorkspaceHandle, kind string) connectorapi.ConnectorDataRuntime {
	return connectorports.DataRuntime(s.connectorWorkspace(runtime), kind)
}

func (s *Server) connectorLiveRuntime(runtime *gatewayinfra.WorkspaceHandle, kind string) connectorapi.LiveConsoleRuntime {
	return connectorports.LiveRuntime(s.connectorWorkspace(runtime), kind)
}

func (s *Server) connectorCredentialResourceRuntime(runtime *gatewayinfra.WorkspaceHandle, kind string) connectorapi.CredentialResourceRuntime {
	return connectorports.PortCredentialResourceRuntime(s.connectorWorkspace(runtime), kind)
}

func (s *Server) connectorTargetLifecycleRuntime(runtime *gatewayinfra.WorkspaceHandle, kind string) connectorapi.TargetLifecycleRuntime {
	return s.connectorPortsApplication().TargetLifecycleRuntime(s.connectorWorkspace(runtime), kind)
}
