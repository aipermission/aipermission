package api

import (
	"context"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

type declineConnectorActionApprovalRequest = gatewayaccess.ConnectorApprovalNoteRequest
type runConnectorActionApprovalRequest = gatewayaccess.ConnectorApprovalNoteRequest
type connectorActionApprovalItem = gatewayaccess.ConnectorApprovalItem

func connectorActionApprovalItemFromRequest(item connectormgmt.ActionRequest) connectorActionApprovalItem {
	return gatewayaccess.ConnectorApprovalItemFromRequest(item)
}

func (s *Server) runPendingConnectorAction(ctx context.Context, runtime *databaseRuntime, id int64, userNote string) (connectormgmt.ActionRequest, error) {
	workflow, err := s.connectorActionWorkflow(runtime)
	if err != nil {
		return connectormgmt.ActionRequest{}, err
	}
	return workflow.RunPending(ctx, id, userNote)
}

func (s *Server) connectorActionApprovalItemForResponse(ctx context.Context, runtime *databaseRuntime, item connectormgmt.ActionRequest) (connectorActionApprovalItem, error) {
	workflow, err := s.connectorActionWorkflow(runtime)
	if err != nil {
		return connectorActionApprovalItem{}, err
	}
	return gatewayaccess.ConnectorApprovalItemForResponse(ctx, workflow, item)
}
