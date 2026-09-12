package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) configureConnectorActionApplication() error {
	application, err := s.connectorActionOwner.NewConnectorActionApplication(connectorActionJSONBodyBytes, gatewayinfra.ConnectorActionPorts{
		Capabilities: func(runtime *gatewayinfra.WorkspaceHandle, kind string, dependencies []connectors.ResolvedDependency, finish gatewayinfra.ConnectorActionFinishPort) connectors.RuntimeCapabilityResolver {
			return s.connectorRuntime.ActionCapabilities(runtime, kind, dependencies, finish)
		},
		SupportsRunning: func(prepared gatewayactions.PreparedRequest) bool {
			return s != nil && s.connectorRuntime.SupportsRunning(prepared)
		},
		FinishRunning: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, id int64, prepared gatewayactions.PreparedRequest, principal gatewayaccess.Principal, handles connectors.ActionHandles, finish gatewayinfra.ConnectorActionFinishPort) {
			s.connectorRuntime.FinishRunning(ctx, runtime, id, prepared, principal, handles, finish)
		},
	})
	if err != nil {
		return err
	}
	s.connectorActions = application
	return nil
}

func (s *Server) connectorActionApprovalWorkflow(runtime *gatewayinfra.WorkspaceHandle) (connectormgmt.ConnectorApprovalWorkflow, error) {
	workflow, err := s.connectorActions.Approval(runtime)
	if err != nil {
		return nil, err
	}
	return connectorApprovalWorkflowAdapter{workflow: workflow}, nil
}

type connectorApprovalWorkflowAdapter struct {
	workflow gatewayactions.ApprovalWorkflow
}

func (adapter connectorApprovalWorkflowAdapter) ApprovalPreview(ctx context.Context, request connectormgmt.ActionRequest) (map[string]any, error) {
	return adapter.workflow.ApprovalPreview(ctx, connectormgmt.ReleaseActionRequest(request))
}

func (adapter connectorApprovalWorkflowAdapter) RunPending(ctx context.Context, id int64, note string) (connectormgmt.ActionRequest, error) {
	request, err := adapter.workflow.RunPending(ctx, id, note)
	return connectormgmt.AdoptActionRequest(request), err
}

func (adapter connectorApprovalWorkflowAdapter) DeclinePending(ctx context.Context, id int64, note string) (connectormgmt.ActionRequest, error) {
	request, err := adapter.workflow.DeclinePending(ctx, id, note)
	return connectormgmt.AdoptActionRequest(request), err
}

func (s *Server) connectorActionShutdownWorkflow(runtime *gatewayinfra.WorkspaceHandle) (gatewayactions.ShutdownWorkflow, error) {
	return s.connectorActions.Shutdown(runtime)
}
