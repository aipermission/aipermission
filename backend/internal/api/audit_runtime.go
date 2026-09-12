package api

import (
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) configureAuditDispatcher(runtime *gatewayinfra.WorkspaceHandle) {
	s.infrastructure.ConfigureObservationDispatcher(runtime)
}
