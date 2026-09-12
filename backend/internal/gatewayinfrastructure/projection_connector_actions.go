package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
)

var ErrWorkspaceHandleUnavailable = errors.New("workspace handle is unavailable")

type ConnectorActionPorts struct {
	Capabilities  func(string, []connectors.ResolvedDependency) connectors.RuntimeCapabilityResolver
	FinishRunning gatewayactions.FinishRunning
}

func (component *ConnectorActionOwner) ConnectorActionWorkspace(handle *WorkspaceHandle, ports ConnectorActionPorts) (gatewayactions.Workspace, bool) {
	owner, ok := component.resolve(handle)
	if !ok || owner.Security.PolicyService() == nil {
		return gatewayactions.Workspace{}, false
	}
	identity := handle.Identity()
	observation := component.owner.ObservationOwner()
	workflow := gatewayactions.WorkflowPorts{
		AcquireSecret: owner.Security.VaultDeliveryCoordinator().AcquireDelivery,
		RedactBasic: func(ctx context.Context, value string) string {
			return owner.Security.PolicyService().Redact(ctx, value)
		},
		RedactCustom: func(ctx context.Context, value string) string {
			return owner.Security.PolicyService().RedactCustom(ctx, value)
		},
		Mutate: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return observation.WithObservationMutation(ctx, handle, actor, tokenID, runtimeID, action, payload, mutate)
		},
		Transaction: func(ctx context.Context, mutate func(*sql.Tx, gatewayactions.AuditAppender) error) error {
			return observation.WithObservationTransaction(ctx, handle, func(tx *sql.Tx, appendAudit ObservationAppender) error {
				return mutate(tx, gatewayactions.AuditAppender(appendAudit))
			})
		},
		Observe: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			observation.WriteObservation(ctx, handle, actor, tokenID, runtimeID, action, payload)
		},
		Capabilities: ports.Capabilities, FinishRunning: ports.FinishRunning,
	}
	return gatewayactions.Workspace{
		Storage: gatewayactions.ActionStorage{
			Database: owner.Storage.DatabaseHandle(), Tokens: owner.Storage.TokenStore(),
			Registry: owner.Connectors.ConnectorRegistry(), SecretVault: owner.Storage.SecretVault(),
			WorkspaceID: identity.WorkspaceID,
		},
		Identity: gatewayactions.ActionIdentity{
			Tag: owner.Tag, RuntimeInstanceID: identity.RuntimeID,
			MCPStarted: owner.Security.RuntimeControlState().MCPStarted,
			Ensure: func() error {
				if current, valid := component.resolve(handle); !valid || current != owner || !handle.Identity().Ready() {
					return ErrWorkspaceHandleUnavailable
				}
				return nil
			},
		},
		Workflow: workflow,
	}, true
}
