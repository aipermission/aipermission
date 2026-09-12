package gatewayinfrastructure

import (
	"errors"

	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
)

var ErrWorkspaceHandleUnavailable = errors.New("workspace handle is unavailable")

func (component *ConnectorActionOwner) ConnectorActionWorkspace(handle *WorkspaceHandle, workflow gatewayactions.WorkflowPorts) (gatewayactions.Workspace, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return gatewayactions.Workspace{}, false
	}
	identity := handle.Identity()
	workflow.AcquireSecret = owner.Security.VaultDeliveryCoordinator().AcquireDelivery
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
