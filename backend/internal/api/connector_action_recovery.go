package api

import gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"

func (s *Server) startConnectorActionRecoveryWorker(runtime *gatewayinfra.WorkspaceHandle) {
	s.connectorActions.StartRecovery(runtime)
}

func (s *Server) stopConnectorActionRecoveryWorker(runtime *gatewayinfra.WorkspaceHandle) {
	s.connectorActions.StopRecovery(runtime)
}
