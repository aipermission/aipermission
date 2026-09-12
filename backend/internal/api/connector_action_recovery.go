package api

import gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"

func (s *Server) startConnectorActionRecoveryWorker(runtime *gatewayinfra.WorkspaceHandle) {
	s.connectorActionApplication().StartRecovery(s.connectorActionWorkspace(runtime))
}

func (s *Server) stopConnectorActionRecoveryWorker(runtime *gatewayinfra.WorkspaceHandle) {
	s.connectorActionApplication().StopRecovery(s.connectorActionWorkspace(runtime))
}
