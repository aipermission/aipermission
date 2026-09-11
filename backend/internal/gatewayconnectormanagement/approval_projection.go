package gatewayconnectormanagement

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectorapproval"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (*Component) ConnectorApprovalItemForResponse(ctx context.Context, workflow connectorapproval.Workflow, item connectortargets.ActionRequest) (connectorapproval.Item, error) {
	return connectorapproval.ItemForResponse(ctx, workflow, item)
}

func (*Component) ConnectorApprovalItemFromRequest(item connectortargets.ActionRequest) connectorapproval.Item {
	return connectorapproval.ItemFromRequest(item)
}
