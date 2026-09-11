package api

import (
	"context"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

type declineConnectorActionApprovalRequest = connectormgmt.ConnectorApprovalNoteRequest
type runConnectorActionApprovalRequest = connectormgmt.ConnectorApprovalNoteRequest
type connectorActionApprovalItem = connectormgmt.ConnectorApprovalItem

func (s *Server) connectorActionApprovalItemFromRequest(item connectormgmt.ActionRequest) connectorActionApprovalItem {
	return s.connectorManagementApplication().ConnectorApprovalItemFromRequest(item)
}

func (s *Server) runPendingConnectorAction(ctx context.Context, runtime databaseRuntime, id int64, userNote string) (connectormgmt.ActionRequest, error) {
	workflow, err := s.connectorActionWorkflow(runtime)
	if err != nil {
		return connectormgmt.ActionRequest{}, err
	}
	return workflow.RunPending(ctx, id, userNote)
}

func (s *Server) connectorActionApprovalItemForResponse(ctx context.Context, runtime databaseRuntime, item connectormgmt.ActionRequest) (connectorActionApprovalItem, error) {
	workflow, err := s.connectorActionWorkflow(runtime)
	if err != nil {
		return connectorActionApprovalItem{}, err
	}
	return s.connectorManagementApplication().ConnectorApprovalItemForResponse(ctx, workflow, item)
}
