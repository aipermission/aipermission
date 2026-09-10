package api

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/actions"
	applicationactions "github.com/aipermission/aipermission/backend/internal/applicationconnectoractions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

func (s *Server) connectorActionApplication() *applicationactions.Component {
	return applicationactions.New(applicationactions.Dependencies{
		MaxJSONBytes: connectorActionJSONBodyBytes, EnsureIdentity: ensureRuntimeIdentity,
		RedactBasic: func(ctx context.Context, runtime *workspaceruntime.Runtime, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		RedactCustom: func(ctx context.Context, runtime *workspaceruntime.Runtime, value string) string {
			return s.redactCustom(ctx, runtime, value)
		},
		Mutate: func(ctx context.Context, runtime *workspaceruntime.Runtime, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, actor, tokenID, runtimeID, action, payload, mutate)
		},
		Transaction: func(ctx context.Context, runtime *workspaceruntime.Runtime, mutate func(*sql.Tx, actions.AuditAppender) error) error {
			return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
				return mutate(tx, actions.AuditAppender(appendAudit))
			})
		},
		Observe: func(ctx context.Context, runtime *workspaceruntime.Runtime, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
		},
		Capabilities: func(kind string, runtime *workspaceruntime.Runtime, dependencies []actions.ResolvedDependency) connectors.RuntimeCapabilityResolver {
			return connectorRuntimeCapabilitiesForAction(kind, s, runtime, dependencies)
		},
		SupportsRunning: s.connectorActionSupportsRunning,
		FinishRunning: func(runtime *workspaceruntime.Runtime, id int64, prepared actions.PreparedRequest, principal executionprincipal.Principal, handles connectors.ActionHandles) {
			s.finishActiveConnectorActionRequest(runtime, id, prepared, principal, handles)
		},
	})
}

func (s *Server) connectorActionWorkflow(runtime *databaseRuntime) (*actions.Runtime, error) {
	return s.connectorActionApplication().Workflow(runtime)
}
