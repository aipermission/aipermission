package api

import (
	"context"
	"database/sql"
	"errors"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

var errAuditedMutationUnchanged = errors.New("audited mutation unchanged")

// writeObservationAudit records telemetry that is not the durable proof of a
// local domain mutation. Mutations must use withAuditedMutation, a
// transaction-aware store hook, or an approved lifecycle trigger instead.
func (s *Server) writeObservationAudit(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, actorType string, tokenID *int64, runtimeID int64, action string, payload any) {
	s.infrastructure.WriteObservation(ctx, runtime, actorType, tokenID, runtimeID, action, payload)
}

func (s *Server) writeAuditRequired(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, actorType string, tokenID *int64, runtimeID int64, action string, payload any) error {
	return s.infrastructure.WriteObservationRequired(ctx, runtime, actorType, tokenID, runtimeID, action, payload)
}

func (s *Server) prepareAuditRedactor(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle) func(string) string {
	return s.infrastructure.PrepareObservationRedactor(ctx, runtime)
}

type auditAppender func(*sql.Tx, string, *int64, int64, string, any) error

func (s *Server) withAuditedTransaction(
	ctx context.Context,
	runtime *gatewayinfra.WorkspaceHandle,
	mutate func(*sql.Tx, auditAppender) error,
) error {
	return s.infrastructure.WithObservationTransaction(ctx, runtime, func(tx *sql.Tx, appendObservation gatewayinfra.ObservationAppender) error {
		return mutate(tx, auditAppender(appendObservation))
	})
}

func (s *Server) withAuditedMutation(
	ctx context.Context,
	runtime *gatewayinfra.WorkspaceHandle,
	actorType string,
	tokenID *int64,
	runtimeID int64,
	action string,
	payload func() any,
	mutate func(*sql.Tx) error,
) error {
	return s.infrastructure.WithObservationMutation(ctx, runtime, actorType, tokenID, runtimeID, action, payload, mutate)
}

func (s *Server) projectAuditEvents(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle) {
	s.infrastructure.ProjectObservations(ctx, runtime)
}

func int64Ptr(value int64) *int64 {
	return &value
}
