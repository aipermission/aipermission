package api

import applicationactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"

func (s *Server) startConnectorActionRecoveryWorker(runtime databaseRuntime) {
	s.connectorActionApplication().StartRecovery(s.connectorActionWorkspace(runtime))
}

func (s *Server) stopConnectorActionRecoveryWorker(runtime databaseRuntime) {
	applicationactions.StopRecovery(s.connectorActionWorkspace(runtime))
}
