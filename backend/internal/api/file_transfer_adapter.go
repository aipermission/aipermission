package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
	gatewaytransfer "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
)

func (s *Server) fileTransferWorkspace(w http.ResponseWriter) (gatewaytransfer.FileTransferWorkspace, bool) {
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
	return gatewaytransfer.InitializeFileTransferWorkspace(
		runtime,
		runtime.StoragePort().DatabaseHandle(),
		func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
		},
		func(ctx context.Context, runtimeID int64) (gatewaytransfer.FileTransferConnectorPorts, error) {
			return connectorFileTransferPortsForID(ctx, s, runtime, runtimeID)
		},
	)
}

func (s *Server) stopFileTransferRuntime(runtime databaseRuntime) {
	gatewaytransfer.StopFileTransferWorkspace(runtime)
}

func connectorFileTransferPortsForID(ctx context.Context, server *Server, runtime databaseRuntime, runtimeID int64) (gatewaytransfer.FileTransferConnectorPorts, error) {
	if server == nil || runtime == nil || runtime.StoragePort().DatabaseHandle() == nil || runtime.StoragePort().SecretVault() == nil {
		return gatewaytransfer.FileTransferConnectorPorts{}, fmt.Errorf("file transfer connector runtime is unavailable")
	}
	target, _, _, err := server.connectorCatalog(runtime).TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return gatewaytransfer.FileTransferConnectorPorts{}, err
	}
	boundary := gatewaytransfer.NewCredentialBoundary(nil)
	transferRuntime := connectorports.TransferRuntimeWithSecretAccessor(server.connectorWorkspace(runtime), target.ConnectorKind, func(secrets map[string]any) connectors.SecretAccessor {
		boundary.AddStructured(secrets)
		return connectorSecretAccessor{values: secrets, boundary: boundary}
	})
	return gatewaytransfer.FileTransferConnectorPorts{
		ConnectorKind:      target.ConnectorKind,
		Gateway:            server.connectorPortsApplication().FileTransferGateway(server.connectorPortsWorkspace(runtime), target.ConnectorKind),
		Runtime:            transferRuntime,
		CredentialBoundary: boundary,
	}, nil
}

func (s *Server) fileTransferHTTPHandlers() *gatewaytransfer.FileTransferHTTPHandlers {
	return gatewaytransfer.NewFileTransferHTTPHandlers(gatewaytransfer.FileTransferHTTPDependencies{
		Scope:      s.fileTransferWorkspace,
		AdapterFor: s.connectorFileTransferAdapterFor,
		DataPath:   s.config.DataPath,
	})
}
