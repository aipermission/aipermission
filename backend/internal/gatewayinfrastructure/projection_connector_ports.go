package gatewayinfrastructure

import connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"

func (component *ConnectorPortsOwner) connectorWorkspace(handle *WorkspaceHandle, ports connectorports.Workspace) (connectorports.Workspace, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return connectorports.Workspace{}, false
	}
	projected := capabilities.Transport
	capability, ok := projected.Current()
	if !ok || capability.Delivery == nil {
		return connectorports.Workspace{}, false
	}
	workspace := connectorports.NewWorkspace(
		capability.Scopes,
		capability.Database,
		capability.Delivery.AcquireDelivery,
	)
	workspace.Principal = ports.Principal
	workspace.Actions = ports.Actions
	workspace.Transfers = ports.Transfers
	workspace.Targets = ports.Targets
	return workspace, true
}
