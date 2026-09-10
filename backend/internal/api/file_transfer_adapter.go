package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	filetransferhttp "github.com/aipermission/aipermission/backend/internal/filetransfer/httpapi"
)

func (s *Server) fileTransferHTTPRuntime(w http.ResponseWriter) (*filetransferhttp.Runtime, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	if runtime.fileTransfers == nil {
		writeInternalError(w)
		return nil, false
	}
	return runtime.fileTransfers, true
}

func (s *Server) initializeFileTransferRuntime(runtime *databaseRuntime) error {
	if runtime == nil {
		return fmt.Errorf("file transfer workspace runtime is unavailable")
	}
	transferRuntime, err := filetransferhttp.NewRuntime(filetransferhttp.RuntimeDependencies{
		Database:     runtime.database,
		Jobs:         &runtime.transferJobs,
		Finalization: runtime.finalization,
		Observe: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
		},
		ConnectorPorts: func(ctx context.Context, runtimeID int64) (filetransferhttp.ConnectorPorts, error) {
			return connectorFileTransferPortsForID(ctx, s, runtime, runtimeID)
		},
	})
	if err != nil {
		return err
	}
	runtime.fileTransfers = transferRuntime
	return nil
}

func connectorFileTransferPortsForID(ctx context.Context, server *Server, runtime *databaseRuntime, runtimeID int64) (filetransferhttp.ConnectorPorts, error) {
	if server == nil || runtime == nil || runtime.database == nil || runtime.vault == nil {
		return filetransferhttp.ConnectorPorts{}, fmt.Errorf("file transfer connector runtime is unavailable")
	}
	target, _, _, err := connectortargets.NewStore(runtime.database).TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return filetransferhttp.ConnectorPorts{}, err
	}
	boundary := actionresult.NewCredentialBoundary(nil)
	scope := connectorRuntimeScopeWithSecretAccessor(runtime, target.ConnectorKind, func(secrets map[string]any) connectors.SecretAccessor {
		boundary.AddStructured(secrets)
		return connectorSecretAccessor{values: secrets, boundary: boundary}
	})
	return filetransferhttp.ConnectorPorts{
		ConnectorKind:      target.ConnectorKind,
		Gateway:            connectorFileTransferGatewayPort{connectorPeerGatewayPort: connectorPeerGatewayPort{server: server}, runtime: runtime, kind: target.ConnectorKind},
		Runtime:            scope.TransferRuntime(),
		CredentialBoundary: boundary,
	}, nil
}

func (s *Server) fileTransferHTTPHandlers() *filetransferhttp.Handlers {
	return filetransferhttp.NewHandlers(filetransferhttp.Dependencies{
		Scope:      s.fileTransferHTTPRuntime,
		AdapterFor: s.connectorFileTransferAdapterFor,
		DataPath:   s.config.DataPath,
	})
}
