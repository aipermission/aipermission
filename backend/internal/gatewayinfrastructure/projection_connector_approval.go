package gatewayinfrastructure

import (
	"context"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

type ConnectorApprovalPorts struct {
	Workflow func() (connectormgmt.ConnectorApprovalWorkflow, error)
	Redact   func(context.Context, string) string
}

func (component *ConnectorActionOwner) ConnectorApprovalWorkspace(handle *WorkspaceHandle, ports ConnectorApprovalPorts) (connectormgmt.ConnectorApprovalScope, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return connectormgmt.ConnectorApprovalScope{}, false
	}
	projected := capabilities.Approval
	capability, ok := projected.Current()
	if !ok || capability.Database == nil || capability.Control == nil {
		return connectormgmt.ConnectorApprovalScope{}, false
	}
	return connectormgmt.ConnectorApprovalScope{
		Requests: connectormgmt.NewConnectorApprovalRequestStore(capability.Database), Workflow: ports.Workflow,
		MCPStarted: capability.Control.MCPStarted, Redact: ports.Redact,
	}, true
}
