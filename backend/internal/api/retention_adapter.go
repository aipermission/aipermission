package api

func (s *Server) initializeRetention(runtime databaseRuntime) {
	s.observation.InitializeRetention(observationRuntime(runtime), func() {
		s.startConnectorActionRecoveryWorker(runtime)
	})
}
