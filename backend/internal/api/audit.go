package api

import (
	"context"
	"database/sql"
	"errors"

	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

var errAuditedMutationUnchanged = errors.New("audited mutation unchanged")

// writeObservationAudit records telemetry that is not the durable proof of a
// local domain mutation. Mutations must use withAuditedMutation, a
// transaction-aware store hook, or an approved lifecycle trigger instead.
func (s *Server) writeObservationAudit(ctx context.Context, runtime databaseRuntime, actorType string, tokenID *int64, runtimeID int64, action string, payload any) {
	s.observation.WriteObservation(ctx, runtime, actorType, tokenID, runtimeID, action, payload)
}

func (s *Server) writeAuditRequired(ctx context.Context, runtime databaseRuntime, actorType string, tokenID *int64, runtimeID int64, action string, payload any) error {
	return s.observation.WriteRequired(ctx, runtime, actorType, tokenID, runtimeID, action, payload)
}

func (s *Server) prepareAuditRedactor(ctx context.Context, runtime databaseRuntime) func(string) string {
	return s.observation.PrepareRedactor(ctx, runtime)
}

type auditAppender = gatewayoperations.ObservationAppender

func (s *Server) withAuditedTransaction(
	ctx context.Context,
	runtime databaseRuntime,
	mutate func(*sql.Tx, auditAppender) error,
) error {
	return s.observation.WithTransaction(ctx, runtime, mutate)
}

func (s *Server) withAuditedMutation(
	ctx context.Context,
	runtime databaseRuntime,
	actorType string,
	tokenID *int64,
	runtimeID int64,
	action string,
	payload func() any,
	mutate func(*sql.Tx) error,
) error {
	return s.observation.WithMutation(ctx, runtime, actorType, tokenID, runtimeID, action, payload, mutate)
}

func (s *Server) projectAuditEvents(ctx context.Context, runtime databaseRuntime) {
	s.observation.Project(ctx, runtime)
}

func int64Ptr(value int64) *int64 {
	return &value
}
