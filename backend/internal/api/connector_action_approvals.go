package api

import (
	"context"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) connectorActionApprovalItemFromRequest(item connectormgmt.ActionRequest) connectormgmt.ConnectorApprovalItem {
	return s.connectorManagementApplication().ConnectorApprovalItemFromRequest(item)
}

func (s *Server) runPendingConnectorAction(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, id int64, userNote string) (connectormgmt.ActionRequest, error) {
	workflow, err := s.connectorActionApprovalWorkflow(runtime)
	if err != nil {
		return connectormgmt.ActionRequest{}, err
	}
	return workflow.RunPending(ctx, id, userNote)
}

func (s *Server) connectorActionApprovalItemForResponse(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, item connectormgmt.ActionRequest) (connectormgmt.ConnectorApprovalItem, error) {
	workflow, err := s.connectorActionApprovalWorkflow(runtime)
	if err != nil {
		return connectormgmt.ConnectorApprovalItem{}, err
	}
	return s.connectorManagementApplication().ConnectorApprovalItemForResponse(ctx, workflow, item)
}
