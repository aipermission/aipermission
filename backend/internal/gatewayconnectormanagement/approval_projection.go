package gatewayconnectormanagement

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectorapproval"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (*Component) ConnectorApprovalItemForResponse(ctx context.Context, workflow ConnectorApprovalWorkflow, item ActionRequest) (ConnectorApprovalItem, error) {
	result, err := connectorapproval.ItemForResponse(ctx, domainApprovalWorkflow{workflow}, item.domain())
	return ConnectorApprovalItem(result), err
}

type domainApprovalWorkflow struct{ workflow ConnectorApprovalWorkflow }

func (adapter domainApprovalWorkflow) ApprovalPreview(ctx context.Context, request connectortargets.ActionRequest) (map[string]any, error) {
	return adapter.workflow.ApprovalPreview(ctx, actionRequestFromDomain(request))
}

func (adapter domainApprovalWorkflow) RunPending(ctx context.Context, id int64, note string) (connectortargets.ActionRequest, error) {
	request, err := adapter.workflow.RunPending(ctx, id, note)
	return request.domain(), err
}

func (adapter domainApprovalWorkflow) DeclinePending(ctx context.Context, id int64, note string) (connectortargets.ActionRequest, error) {
	request, err := adapter.workflow.DeclinePending(ctx, id, note)
	return request.domain(), err
}

func (*Component) ConnectorApprovalItemFromRequest(item ActionRequest) ConnectorApprovalItem {
	return ConnectorApprovalItem(connectorapproval.ItemFromRequest(item.domain()))
}
