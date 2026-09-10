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
	runtime.actionWorkflowMu.Lock()
	workflow := runtime.actionWorkflow
	runtime.actionWorkflowMu.Unlock()
	if workflow != nil {
		workflow.StopRecovery()
	}
}
