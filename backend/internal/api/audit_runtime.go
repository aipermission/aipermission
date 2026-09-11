package api

func (s *Server) configureAuditDispatcher(runtime databaseRuntime) {
	s.observation.ConfigureDispatcher(runtime)
}
