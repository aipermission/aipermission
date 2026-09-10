package api

func (s *Server) initializeRetention(runtime *databaseRuntime) {
	s.observation.InitializeRetention(runtime, s.startConnectorActionRecoveryWorker)
}
