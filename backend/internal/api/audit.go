package api

import (
	"context"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

// writeObservationAudit records telemetry that is not the durable proof of a
// local domain mutation. Mutations must use an owner-provided transactional
// boundary, a transaction-aware store hook, or an approved lifecycle trigger.
func (s *Server) writeObservationAudit(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, actorType string, tokenID *int64, runtimeID int64, action string, payload any) {
	s.observationOwner.WriteObservation(ctx, runtime, actorType, tokenID, runtimeID, action, payload)
}

func (s *Server) writeAuditRequired(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, actorType string, tokenID *int64, runtimeID int64, action string, payload any) error {
	return s.observationOwner.WriteObservationRequired(ctx, runtime, actorType, tokenID, runtimeID, action, payload)
}

func (s *Server) prepareAuditRedactor(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle) func(string) string {
	return s.observationOwner.PrepareObservationRedactor(ctx, runtime)
}

func (s *Server) projectAuditEvents(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle) {
	s.observationOwner.ProjectObservations(ctx, runtime)
}

func int64Ptr(value int64) *int64 {
	return &value
}
