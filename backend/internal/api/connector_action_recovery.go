package api

func (s *Server) startConnectorActionRecoveryWorker(runtime *databaseRuntime) {
	workflow, err := s.connectorActionWorkflow(runtime)
	if err == nil {
		workflow.StartRecovery()
	}
}

func (s *Server) stopConnectorActionRecoveryWorker(runtime *databaseRuntime) {
	if runtime == nil {
		return
	}
	workflow := runtime.Operations.ActionWorkflow()
	if workflow != nil {
		workflow.StopRecovery()
	}
}
