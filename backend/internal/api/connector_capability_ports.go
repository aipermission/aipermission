package api

import (
	"context"
	"net/http"

	connectorports "github.com/aipermission/aipermission/backend/internal/applicationconnectorports"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

func (s *Server) connectorPortsApplication() *connectorports.Component {
	return connectorports.New(connectorports.Dependencies{
		TrustStorePath: s.connectorTrustStorePath,
		ActiveRuntime: func(w http.ResponseWriter) bool {
			_, ok := s.activeRuntimeOrLocked(w)
			return ok
		},
		ChangePeerTrust: s.connectorChangeVaultPeerTrust,
		LocalPrincipal: func(runtime *workspaceruntime.Runtime) (executionprincipal.Principal, error) {
			if runtime == nil {
				return executionprincipal.Principal{}, errInvalidConnectorRuntime
			}
			return localExecutionPrincipal(runtime)
		},
		LiveTransportAdapter: s.connectorLiveConsoleTransportAdapterFor,
		LiveTargetAdapter:    s.connectorLiveConsoleTargetAdapterFor,
		RestartConsole: func(ctx context.Context, runtime *workspaceruntime.Runtime, principal executionprincipal.Principal, runtimeID int64, runningError string) (connectorapi.ConsoleRestartResult, error) {
			result, err := s.restartServerConsoleSession(ctx, runtime, principal, runtimeID, runningError)
			return connectorapi.ConsoleRestartResult{ClosedSessionIDs: result.ClosedSessionIDs, CanceledRunningRequests: result.CanceledRunningRequests}, err
		},
		RunDownloadBatch: func(ctx context.Context, runtime *workspaceruntime.Runtime, authorization connectorapi.TransferAuthorization, runtimeID int64, paths []string, archiveName, source string) (connectorapi.TransferBatch, error) {
			if runtime == nil || runtime.Operations.FileTransfers == nil {
				return connectorapi.TransferBatch{}, errInvalidConnectorRuntime
			}
			batch, err := s.fileTransferHTTPHandlers().CreateAndLaunchDownloadBatch(ctx, runtime.Operations.FileTransfers, authorization, runtimeID, paths, archiveName, source)
			return connectorapi.TransferBatch{ID: batch.ID, Status: batch.Status, ItemCount: len(batch.Items)}, err
		},
		FinishAction: s.finishConnectorActionRequest,
		RuntimeCapabilities: func(kind string, runtime *workspaceruntime.Runtime) connectors.RuntimeCapabilityResolver {
			return connectorRuntimeCapabilitiesFor(kind, s, runtime)
		},
		DeleteTarget: func(ctx context.Context, runtime *workspaceruntime.Runtime, target connectortargets.Target, payload map[string]any) error {
			return (connectorTargetHandlers{s}).connectorDeleteTargetRecord(ctx, runtime, target, payload)
		},
		FinalizeDeletedTarget: func(ctx context.Context, runtime *workspaceruntime.Runtime, target connectortargets.Target, reason string, payload map[string]any) (int64, error) {
			return (connectorTargetHandlers{s}).connectorFinalizeDeletedTarget(ctx, runtime, target, reason, payload)
		},
		WriteAudit: s.writeObservationAudit,
	})
}

func connectorDataRuntimePort(runtime *databaseRuntime, kind string) connectorapi.ConnectorDataRuntime {
	return connectorports.DataRuntime(runtime, kind)
}

func connectorLiveRuntime(runtime *databaseRuntime, kind string) connectorapi.LiveConsoleRuntime {
	return connectorports.LiveRuntime(runtime, kind)
}

func connectorActionRuntime(runtime *databaseRuntime, kind string) connectorapi.ActionRuntime {
	return connectorports.ActionRuntime(runtime, kind)
}

func connectorCredentialResourceRuntime(runtime *databaseRuntime, kind string) connectorapi.CredentialResourceRuntime {
	return connectorports.CredentialResourceRuntime(runtime, kind)
}

func (s *Server) connectorTargetLifecycleRuntime(runtime *databaseRuntime, kind string) connectorapi.TargetLifecycleRuntime {
	return s.connectorPortsApplication().TargetLifecycleRuntime(runtime, kind)
}

func connectorRuntimeIDBelongsToKind(ctx context.Context, runtime *databaseRuntime, kind string, runtimeID int64) error {
	return connectorports.RequireRuntimeID(ctx, runtime, kind, runtimeID)
}

func connectorRuntimeIDBelongsToTarget(ctx context.Context, runtime *databaseRuntime, kind string, targetID, runtimeID int64) error {
	return connectorports.RequireTargetRuntimeID(ctx, runtime, kind, targetID, runtimeID)
}
