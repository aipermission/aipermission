package api

import (
	"context"

	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func observationRuntime(runtime databaseRuntime) gatewayoperations.ObservationRuntime {
	if runtime == nil {
		return gatewayoperations.ObservationRuntime{}
	}
	return gatewayoperations.ObservationRuntime{
		Database:   runtime.Storage.DatabaseHandle(),
		DatabaseID: runtime.Identity.DatabaseID,
		Registry:   runtime.Connectors.ConnectorRegistry(),
		MCPStarted: runtime.Security.RuntimeControlState().MCPStarted(),
		PrepareRedactor: func(ctx context.Context) func(string) string {
			if runtime.Security.PolicyService() == nil {
				return nil
			}
			return runtime.Security.PolicyService().PrepareRedactor(ctx)
		},
		AuditDispatcher:     runtime.Observation.AuditDispatcherService,
		SetAuditDispatcher:  runtime.Observation.SetAuditDispatcherService,
		RetentionService:    runtime.Observation.RetentionService,
		SetRetentionService: runtime.Observation.SetRetentionService,
	}
}

func (s *Server) configureAuditDispatcher(runtime databaseRuntime) {
	s.observation.ConfigureDispatcher(observationRuntime(runtime))
}
