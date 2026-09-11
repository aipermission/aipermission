package api

func (s *Server) startConnectorActionRecoveryWorker(runtime databaseRuntime) {
	s.connectorActionApplication().StartRecovery(s.connectorActionWorkspace(runtime))
}

func (s *Server) stopConnectorActionRecoveryWorker(runtime databaseRuntime) {
	s.connectorActionApplication().StopRecovery(s.connectorActionWorkspace(runtime))
}
