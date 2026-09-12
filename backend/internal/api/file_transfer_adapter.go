package api

import (
	"context"
	"fmt"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) initializeFileTransferRuntime(runtime *gatewayinfra.WorkspaceHandle) error {
	if runtime == nil {
		return fmt.Errorf("file transfer workspace runtime is unavailable")
	}
	return s.operationsOwner.InitializeTransferWorkspace(runtime,
		func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
		},
		func(ctx context.Context, runtimeID int64) (gatewayinfra.FileTransferConnectorPorts, error) {
			return s.connectorManagementApplication().FileTransferPorts(ctx, runtime, runtimeID)
		},
	)
}

func (s *Server) stopFileTransferRuntime(runtime *gatewayinfra.WorkspaceHandle) {
	s.operationsOwner.StopTransferWorkspace(runtime)
}

func (s *Server) fileTransferHTTPHandlers() gatewayinfra.FileTransferHTTPHandlers {
	return s.operationsOwner.FileTransferHTTPHandlers()
}
