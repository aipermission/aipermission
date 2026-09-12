package gatewayinfrastructure

import connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"

func (component *ConnectorPortsOwner) ConnectorPortsWorkspace(handle *WorkspaceHandle, ports connectorports.Workspace) (connectorports.Workspace, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return connectorports.Workspace{}, false
	}
	workspace := connectorports.NewWorkspace(
		owner.Connectors,
		owner.Storage.DatabaseHandle(),
		owner.Security.VaultDeliveryCoordinator().AcquireDelivery,
	)
	workspace.Principal = ports.Principal
	workspace.Actions = ports.Actions
	workspace.Transfers = ports.Transfers
	workspace.Targets = ports.Targets
	return workspace, true
}
