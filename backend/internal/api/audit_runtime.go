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
		Database:   runtime.StoragePort().DatabaseHandle(),
		DatabaseID: runtime.DatabaseIdentifier(),
		Registry:   runtime.ConnectorPort().ConnectorRegistry(),
		MCPStarted: runtime.IsMCPStarted(),
		PrepareRedactor: func(ctx context.Context) func(string) string {
			if runtime.SecurityPort().PolicyService() == nil {
				return nil
			}
			return runtime.SecurityPort().PolicyService().PrepareRedactor(ctx)
		},
		AuditDispatcher:     runtime.ObservationPort().AuditDispatcherService,
		SetAuditDispatcher:  runtime.ObservationPort().SetAuditDispatcherService,
		RetentionService:    runtime.ObservationPort().RetentionService,
		SetRetentionService: runtime.ObservationPort().SetRetentionService,
	}
}

func (s *Server) configureAuditDispatcher(runtime databaseRuntime) {
	s.observation.ConfigureDispatcher(observationRuntime(runtime))
}
