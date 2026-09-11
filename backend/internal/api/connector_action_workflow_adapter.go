package api

import (
	"context"
	"database/sql"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) connectorActionApplication() *actions.Component {
	return actions.New(actions.Dependencies{
		MaxJSONBytes: connectorActionJSONBodyBytes, EnsureIdentity: ensureRuntimeIdentity,
		RedactBasic: func(ctx context.Context, runtime gatewayinfra.Runtime, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		RedactCustom: func(ctx context.Context, runtime gatewayinfra.Runtime, value string) string {
			return s.redactCustom(ctx, runtime, value)
		},
		Mutate: func(ctx context.Context, runtime gatewayinfra.Runtime, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, actor, tokenID, runtimeID, action, payload, mutate)
		},
		Transaction: func(ctx context.Context, runtime gatewayinfra.Runtime, mutate func(*sql.Tx, actions.AuditAppender) error) error {
			return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
				return mutate(tx, actions.AuditAppender(appendAudit))
			})
		},
		Observe: func(ctx context.Context, runtime gatewayinfra.Runtime, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
		},
		Capabilities: func(kind string, runtime gatewayinfra.Runtime, dependencies []actions.ResolvedDependency) connectors.RuntimeCapabilityResolver {
			return connectorRuntimeCapabilitiesForAction(kind, s, runtime, dependencies)
		},
		SupportsRunning: s.connectorActionSupportsRunning,
		FinishRunning: func(runtime gatewayinfra.Runtime, id int64, prepared actions.PreparedRequest, principal gatewayaccess.Principal, handles connectors.ActionHandles) {
			s.finishActiveConnectorActionRequest(runtime, id, prepared, principal, handles)
		},
	})
}

func (s *Server) connectorActionWorkflow(runtime databaseRuntime) (*actions.Runtime, error) {
	return s.connectorActionApplication().Workflow(runtime)
}
