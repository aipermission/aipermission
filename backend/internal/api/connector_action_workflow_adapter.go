package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) configureConnectorActionApplication() error {
	application, err := s.connectorActionOwner.NewConnectorActionApplication(connectorActionJSONBodyBytes, gatewayinfra.ConnectorActionPorts{
		Capabilities: func(runtime *gatewayinfra.WorkspaceHandle, kind string, dependencies []connectors.ResolvedDependency) connectors.RuntimeCapabilityResolver {
			return s.connectorRuntime.ActionCapabilities(runtime, kind, dependencies)
		},
		SupportsRunning: func(prepared gatewayactions.PreparedRequest) bool {
			return s != nil && s.connectorRuntime.SupportsRunning(prepared)
		},
		FinishRunning: func(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, id int64, prepared gatewayactions.PreparedRequest, principal gatewayaccess.Principal, handles connectors.ActionHandles) {
			s.connectorRuntime.FinishRunning(ctx, runtime, id, prepared, principal, handles)
		},
	})
	if err != nil {
		return err
	}
	s.connectorActions = application
	return nil
}

func (s *Server) connectorActionApprovalWorkflow(runtime *gatewayinfra.WorkspaceHandle) (gatewayactions.ApprovalWorkflow, error) {
	return s.connectorActions.Approval(runtime)
}

func (s *Server) connectorActionShutdownWorkflow(runtime *gatewayinfra.WorkspaceHandle) (gatewayactions.ShutdownWorkflow, error) {
	return s.connectorActions.Shutdown(runtime)
}
