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
	owner, ok := component.resolve(handle)
	if !ok {
		return connectormgmt.ConnectorApprovalScope{}, false
	}
	return connectormgmt.ConnectorApprovalScope{
		Database: owner.Storage.DatabaseHandle(), Workflow: ports.Workflow,
		MCPStarted: owner.Security.RuntimeControlState().MCPStarted, Redact: ports.Redact,
	}, true
}
