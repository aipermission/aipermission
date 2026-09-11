package api

import (
	"context"
	"fmt"
	"net/http"

	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (s *Server) fileTransferHTTPRuntime(w http.ResponseWriter) (*gatewayoperations.FileTransferRuntime, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	if runtime.OperationsPort().FileTransferRuntime() == nil {
		writeInternalError(w)
		return nil, false
	}
	return runtime.OperationsPort().FileTransferRuntime(), true
}

func (s *Server) initializeFileTransferRuntime(runtime databaseRuntime) error {
	if runtime == nil {
		return fmt.Errorf("file transfer workspace runtime is unavailable")
	}
	if runtime.OperationsPort().FileTransferLifecycle() == nil {
		return fmt.Errorf("file transfer workspace lifecycle is unavailable")
	}
	transferRuntime, err := runtime.OperationsPort().FileTransferLifecycle().NewRuntime(
		runtime.StoragePort().DatabaseHandle(),
		func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
		},
		func(ctx context.Context, runtimeID int64) (gatewayoperations.FileTransferConnectorPorts, error) {
			return connectorFileTransferPortsForID(ctx, s, runtime, runtimeID)
		},
	)
	if err != nil {
		return err
	}
	runtime.OperationsPort().SetFileTransferRuntime(transferRuntime)
	return nil
}

func connectorFileTransferPortsForID(ctx context.Context, server *Server, runtime databaseRuntime, runtimeID int64) (gatewayoperations.FileTransferConnectorPorts, error) {
	if server == nil || runtime == nil || runtime.StoragePort().DatabaseHandle() == nil || runtime.StoragePort().SecretVault() == nil {
		return gatewayoperations.FileTransferConnectorPorts{}, fmt.Errorf("file transfer connector runtime is unavailable")
	}
	target, _, _, err := connectormgmt.NewStore(runtime.StoragePort().DatabaseHandle()).TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return gatewayoperations.FileTransferConnectorPorts{}, err
	}
	boundary := actions.NewCredentialBoundary(nil)
	scope := connectors.ScopeWithSecretAccessor(runtime, target.ConnectorKind, func(secrets map[string]any) connectors.SecretAccessor {
		boundary.AddStructured(secrets)
		return connectorSecretAccessor{values: secrets, boundary: boundary}
	})
	return gatewayoperations.FileTransferConnectorPorts{
		ConnectorKind:      target.ConnectorKind,
		Gateway:            server.connectorPortsApplication().FileTransferGateway(runtime, target.ConnectorKind),
		Runtime:            scope.TransferRuntime(),
		CredentialBoundary: boundary,
	}, nil
}

func (s *Server) fileTransferHTTPHandlers() *gatewayoperations.FileTransferHandlers {
	return gatewayoperations.NewFileTransferHandlers(gatewayoperations.FileTransferDependencies{
		Scope:      s.fileTransferHTTPRuntime,
		AdapterFor: s.connectorFileTransferAdapterFor,
		DataPath:   s.config.DataPath,
	})
}
