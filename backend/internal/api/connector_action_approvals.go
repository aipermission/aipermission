package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectorapproval"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type declineConnectorActionApprovalRequest = connectorapproval.NoteRequest
type runConnectorActionApprovalRequest = connectorapproval.NoteRequest
type connectorActionApprovalItem = connectorapproval.Item

func connectorActionApprovalItemFromRequest(item connectortargets.ActionRequest) connectorActionApprovalItem {
	return connectorapproval.ItemFromRequest(item)
}

func (s *Server) runPendingConnectorAction(ctx context.Context, runtime *databaseRuntime, id int64, userNote string) (connectortargets.ActionRequest, error) {
	workflow, err := s.connectorActionWorkflow(runtime)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return workflow.RunPending(ctx, id, userNote)
}

func (s *Server) connectorActionApprovalItemForResponse(ctx context.Context, runtime *databaseRuntime, item connectortargets.ActionRequest) (connectorActionApprovalItem, error) {
	workflow, err := s.connectorActionWorkflow(runtime)
	if err != nil {
		return connectorActionApprovalItem{}, err
	}
	return connectorapproval.ItemForResponse(ctx, workflow, item)
}
