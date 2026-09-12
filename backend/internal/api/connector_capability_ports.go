package api

import (
	"context"
	"fmt"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) newConnectorRuntimeApplication() *gatewayinfra.ConnectorRuntimeApplication {
	application, err := gatewayinfra.NewConnectorRuntimeApplication(
		s.connectorPortsOwner,
		s.operationsOwner,
		s.connectorAdapterRegistry(),
		gatewayinfra.ConnectorRuntimeDependencies{
			TrustStorePath: s.connectorTrustStorePath,
			ActiveRuntime: func(w http.ResponseWriter) bool {
				_, ok := s.activeRuntimeOrLocked(w)
				return ok
			},
			WorkspaceSnapshot: s.unlockedRuntimeSnapshot,
			InvalidatePeerTrust: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, reason string) error {
				lifecycle, lifecycleErr := s.vaultSessionLifecycle(runtime)
				if lifecycleErr != nil {
					return lifecycleErr
				}
				return lifecycle.InvalidateAll(ctx, reason)
			},
			Execution: gatewayinfra.ConnectorRuntimeExecutionPorts{
				Principal: func(runtime *gatewayinfra.WorkspaceHandle) (gatewayaccess.Principal, error) {
					return s.localExecutionPrincipal(runtime)
				},
				Restart: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, principal gatewayaccess.Principal, runtimeID int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
					result, restartErr := s.restartServerConsoleSession(ctx, runtime, principal, runtimeID, runningError)
					return connectorapi.ConsoleRestartResult{ClosedSessionIDs: result.ClosedSessionIDs, CanceledRunningRequests: result.CanceledRunningRequests}, restartErr
				},
			},
			Transfers: gatewayinfra.ConnectorRuntimeTransferPorts{
				Download: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, authorization connectorapi.TransferAuthorization, runtimeID int64, paths []string, archiveName, source string) (connectorapi.TransferBatch, error) {
					return s.operationsOwner.LaunchTransferDownload(ctx, runtime, authorization, runtimeID, paths, archiveName, source)
				},
			},
			Observation: gatewayinfra.ConnectorRuntimeObservationPorts{
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
