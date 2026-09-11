package api

import (
	"context"
	"database/sql"

	domainactions "github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
)

func (s *Server) connectorActionApplication() *gatewayactions.Component {
	if s != nil && s.connectorActions != nil {
		return s.connectorActions
	}
	return s.newConnectorActionApplication()
}

func (s *Server) newConnectorActionApplication() *gatewayactions.Component {
	return gatewayactions.New(gatewayactions.Dependencies{
		MaxJSONBytes: connectorActionJSONBodyBytes,
		SupportsRunning: func(prepared domainactions.PreparedRequest) bool {
			return s != nil && s.connectorActionSupportsRunning(prepared)
		},
	})
}

func (s *Server) connectorActionWorkspace(runtime databaseRuntime) gatewayactions.Workspace {
	if runtime == nil {
		return gatewayactions.Workspace{}
	}
	delivery := runtime.SecurityPort().VaultDeliveryCoordinator()
	return gatewayactions.Workspace{
		Storage: gatewayactions.ActionStorage{
			Database: runtime.StoragePort().DatabaseHandle(), Tokens: runtime.StoragePort().TokenStore(),
			Registry: runtime.ConnectorPort().ConnectorRegistry(), SecretVault: runtime.StoragePort().SecretVault(),
			WorkspaceID: runtime.WorkspaceIdentifier(),
		},
		Identity: gatewayactions.ActionIdentity{
			Key: runtime.ActionIdentity(), RuntimeInstanceID: runtime.RuntimeIdentifier(),
			MCPStarted: runtime.IsMCPStarted, Ensure: func() error { return ensureRuntimeIdentity(runtime) },
		},
		Workflow: gatewayactions.WorkflowPorts{
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
			Transaction: func(ctx context.Context, mutate func(*sql.Tx, domainactions.AuditAppender) error) error {
				return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
					return mutate(tx, domainactions.AuditAppender(appendAudit))
				})
			},
			Observe: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
				s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
			},
			Capabilities: func(kind string, dependencies []domainactions.ResolvedDependency) connectors.RuntimeCapabilityResolver {
				return connectorRuntimeCapabilitiesForAction(kind, s, runtime, dependencies)
			},
			FinishRunning: func(id int64, prepared domainactions.PreparedRequest, principal gatewayaccess.Principal, handles connectors.ActionHandles) {
				s.finishActiveConnectorActionRequest(runtime, id, prepared, principal, handles)
			},
		},
	}
}

func (s *Server) connectorActionApprovalWorkflow(runtime databaseRuntime) (gatewayactions.ApprovalWorkflow, error) {
	return s.connectorActionApplication().Approval(s.connectorActionWorkspace(runtime))
}

func (s *Server) connectorActionShutdownWorkflow(runtime databaseRuntime) (gatewayactions.ShutdownWorkflow, error) {
	return s.connectorActionApplication().Shutdown(s.connectorActionWorkspace(runtime))
}
