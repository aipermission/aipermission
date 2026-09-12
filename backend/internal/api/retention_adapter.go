package api

import gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"

func (s *Server) initializeRetention(runtime *gatewayinfra.WorkspaceHandle) {
	s.observationOwner.InitializeObservationRetention(runtime, func() {
		s.startConnectorActionRecoveryWorker(runtime)
	})
}
