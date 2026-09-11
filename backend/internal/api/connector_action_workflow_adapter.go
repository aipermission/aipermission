package api

import (
	"context"
	"database/sql"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
)

func (s *Server) connectorActionApplication() *actions.Component {
	return actions.New(actions.Dependencies{
		MaxJSONBytes:    connectorActionJSONBodyBytes,
		SupportsRunning: s.connectorActionSupportsRunning,
	})
}

func (s *Server) connectorActionWorkspace(runtime databaseRuntime) actions.Workspace {
	if runtime == nil {
		return actions.Workspace{}
	}
	delivery := runtime.SecurityPort().VaultDeliveryCoordinator()
	return actions.Workspace{
		Storage: actions.ActionStorage{
			Database: runtime.StoragePort().DatabaseHandle(), Tokens: runtime.StoragePort().TokenStore(),
			Registry: runtime.ConnectorPort().ConnectorRegistry(), SecretVault: runtime.StoragePort().SecretVault(),
			WorkspaceID: runtime.WorkspaceIdentifier(),
		},
		Identity: actions.ActionIdentity{
			Key: runtime.ActionIdentity(), RuntimeInstanceID: runtime.RuntimeIdentifier(),
			MCPStarted: runtime.IsMCPStarted, Ensure: func() error { return ensureRuntimeIdentity(runtime) },
		},
		Workflow: actions.WorkflowPorts{
			Current: runtime.OperationsPort().ActionWorkflow, OrCreate: runtime.OperationsPort().ActionWorkflowOrCreate,
			AcquireSecret: delivery.AcquireDelivery,
			RedactBasic: func(ctx context.Context, value string) string {
				return s.redactForPersistence(ctx, runtime, value)
			},
			RedactCustom: func(ctx context.Context, value string) string {
				return s.redactCustom(ctx, runtime, value)
			},
			Mutate: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
				return s.withAuditedMutation(ctx, runtime, actor, tokenID, runtimeID, action, payload, mutate)
			},
			Transaction: func(ctx context.Context, mutate func(*sql.Tx, actions.AuditAppender) error) error {
				return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
					return mutate(tx, actions.AuditAppender(appendAudit))
				})
			},
			Observe: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
				s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
			},
			Capabilities: func(kind string, dependencies []actions.ResolvedDependency) connectors.RuntimeCapabilityResolver {
				return connectorRuntimeCapabilitiesForAction(kind, s, runtime, dependencies)
			},
			FinishRunning: func(id int64, prepared actions.PreparedRequest, principal gatewayaccess.Principal, handles connectors.ActionHandles) {
				s.finishActiveConnectorActionRequest(runtime, id, prepared, principal, handles)
			},
		},
	}
}

func (s *Server) connectorActionWorkflow(runtime databaseRuntime) (*actions.Runtime, error) {
	return s.connectorActionApplication().Workflow(s.connectorActionWorkspace(runtime))
}
