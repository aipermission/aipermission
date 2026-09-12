package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
)

func (s *Server) newConnectorRuntimeApplication() *gatewayinfra.ConnectorRuntimeApplication {
	application, err := gatewayinfra.NewConnectorRuntimeApplication(
		s.connectorPortsOwner,
		s.connectorAdapterRegistry(),
		gatewayinfra.ConnectorRuntimeDependencies{
			TrustStorePath: s.connectorTrustStorePath,
			ActiveRuntime: func(w http.ResponseWriter) bool {
				_, ok := s.activeRuntimeOrLocked(w)
				return ok
			},
			PeerTrust: s.connectorPeerTrustApplication(),
			Ports: gatewayinfra.ConnectorRuntimePorts{
				Principal: func(runtime *gatewayinfra.WorkspaceHandle) (gatewayaccess.Principal, error) {
					return s.localExecutionPrincipal(runtime)
				},
				Restart: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, principal gatewayaccess.Principal, runtimeID int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
					result, restartErr := s.restartServerConsoleSession(ctx, runtime, principal, runtimeID, runningError)
					return connectorapi.ConsoleRestartResult{ClosedSessionIDs: result.ClosedSessionIDs, CanceledRunningRequests: result.CanceledRunningRequests}, restartErr
				},
				Finish: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, requestID int64, status connectors.ResultStatus, output any, displayText, errorText string, hints ...connectors.OutputHint) (connectormgmt.ActionRequest, error) {
					return s.finishConnectorActionRequest(ctx, runtime, requestID, status, output, displayText, errorText, hints...)
				},
				Download: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, authorization connectorapi.TransferAuthorization, runtimeID int64, paths []string, archiveName, source string) (connectorapi.TransferBatch, error) {
					workspace := fileTransferWorkspaceIdentity(runtime)
					if !s.transfers.WorkspaceReady(workspace) {
						return connectorapi.TransferBatch{}, errInvalidConnectorRuntime
					}
					batch, downloadErr := s.transfers.CreateAndLaunchDownloadBatch(ctx, workspace, s.fileTransferHTTPHandlers(), authorization, runtimeID, paths, archiveName, source)
					return connectorapi.TransferBatch{ID: batch.ID, Status: batch.Status, ItemCount: len(batch.Items)}, downloadErr
				},
				Delete: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, target connectormgmt.Target, payload map[string]any) error {
					return s.connectorManagement.DeleteTarget(ctx, runtime, target, payload)
				},
				Finalize: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, target connectormgmt.Target, reason string, _ map[string]any) (int64, error) {
					return s.connectorManagement.FinalizeDeletedTarget(ctx, runtime, target, reason)
				},
				Audit: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
					s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
				},
			},
		},
	)
	if err != nil {
		panic(fmt.Sprintf("initialize connector runtime application: %v", err))
	}
	return application
}

func (s *Server) connectorPeerTrustApplication() *connectorports.PeerTrustCoordinator {
	return connectorports.NewPeerTrustCoordinator(func() []connectorports.PeerTrustWorkspace {
		runtimes := s.unlockedRuntimeSnapshot()
		workspaces := make([]connectorports.PeerTrustWorkspace, 0, len(runtimes))
		for _, runtime := range runtimes {
			boundRuntime := runtime
			workspace, ok := s.operationsOwner.PeerTrustWorkspace(runtime,
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
