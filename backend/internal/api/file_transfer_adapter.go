package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
	filetransferhttp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/httpapi"
)

func (s *Server) fileTransferWorkspace(w http.ResponseWriter) (filetransferhttp.WorkspaceRuntime, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	return runtime, true
}

func (s *Server) initializeFileTransferRuntime(runtime databaseRuntime) error {
	if runtime == nil {
		return fmt.Errorf("file transfer workspace runtime is unavailable")
	}
	return filetransferhttp.InitializeWorkspaceRuntime(
		runtime,
		runtime.StoragePort().DatabaseHandle(),
		func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
		},
		func(ctx context.Context, runtimeID int64) (filetransferhttp.ConnectorPorts, error) {
			return connectorFileTransferPortsForID(ctx, s, runtime, runtimeID)
		},
	)
}

func (s *Server) stopFileTransferRuntime(runtime databaseRuntime) {
	filetransferhttp.StopWorkspaceRuntime(runtime)
}

func connectorFileTransferPortsForID(ctx context.Context, server *Server, runtime databaseRuntime, runtimeID int64) (filetransferhttp.ConnectorPorts, error) {
	if server == nil || runtime == nil || runtime.StoragePort().DatabaseHandle() == nil || runtime.StoragePort().SecretVault() == nil {
		return filetransferhttp.ConnectorPorts{}, fmt.Errorf("file transfer connector runtime is unavailable")
	}
	target, _, _, err := server.connectorCatalog(runtime).TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return filetransferhttp.ConnectorPorts{}, err
	}
	boundary := server.connectorActionApplication().NewCredentialBoundary(nil)
	transferRuntime := connectorports.TransferRuntimeWithSecretAccessor(server.connectorWorkspace(runtime), target.ConnectorKind, func(secrets map[string]any) connectors.SecretAccessor {
		boundary.AddStructured(secrets)
		return connectorSecretAccessor{values: secrets, boundary: boundary}
	})
	return filetransferhttp.ConnectorPorts{
		ConnectorKind:      target.ConnectorKind,
		Gateway:            server.connectorPortsApplication().FileTransferGateway(server.connectorPortsWorkspace(runtime), target.ConnectorKind),
		Runtime:            transferRuntime,
		CredentialBoundary: boundary,
	}, nil
}

func (s *Server) fileTransferHTTPHandlers() *filetransferhttp.Handlers {
	return filetransferhttp.NewWorkspaceHandlers(filetransferhttp.WorkspaceDependencies{
		Scope:      s.fileTransferWorkspace,
		AdapterFor: s.connectorFileTransferAdapterFor,
		DataPath:   s.config.DataPath,
	})
}
