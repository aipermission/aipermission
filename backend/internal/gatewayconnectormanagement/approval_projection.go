package gatewayconnectormanagement

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectorapproval"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (*Component) ConnectorApprovalItemForResponse(ctx context.Context, workflow ConnectorApprovalWorkflow, item ActionRequest) (ConnectorApprovalItem, error) {
	result, err := connectorapproval.ItemForResponse(ctx, workflow, connectortargets.ActionRequest(item))
	return ConnectorApprovalItem(result), err
}

func (*Component) ConnectorApprovalItemFromRequest(item ActionRequest) ConnectorApprovalItem {
	return ConnectorApprovalItem(connectorapproval.ItemFromRequest(connectortargets.ActionRequest(item)))
}
