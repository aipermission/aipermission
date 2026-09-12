package api

import (
	"context"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
)

func (s *Server) connectorActionApplication() *gatewayactions.Component {
	if s != nil && s.connectorActions != nil {
		return s.connectorActions
	}
	return s.newConnectorActionApplication()
}

func (s *Server) newConnectorActionApplication() *gatewayactions.Component {
	return gatewayactions.New(gatewayactions.Dependencies{
		MaxJSONBytes: connectorActionJSONBodyBytes,
		SupportsRunning: func(prepared gatewayactions.PreparedRequest) bool {
			return s != nil && s.connectorActionSupportsRunning(prepared)
		},
	})
}

func (s *Server) connectorActionWorkspace(runtime *gatewayinfra.WorkspaceHandle) gatewayactions.Workspace {
	if runtime == nil {
		return gatewayactions.Workspace{}
	}
	workspace, ok := s.connectorActionOwner.ConnectorActionWorkspace(runtime, gatewayinfra.ConnectorActionPorts{
		Capabilities: func(kind string, dependencies []connectors.ResolvedDependency) connectors.RuntimeCapabilityResolver {
			return connectorRuntimeCapabilitiesForAction(kind, s, runtime, dependencies)
		},
		FinishRunning: func(ctx context.Context, id int64, prepared gatewayactions.PreparedRequest, principal gatewayaccess.Principal, handles connectors.ActionHandles) {
			s.finishActiveConnectorActionRequest(ctx, runtime, id, prepared, principal, handles)
		},
	})
	if !ok {
		return gatewayactions.Workspace{}
	}
	return workspace
}

func (s *Server) connectorActionApprovalWorkflow(runtime *gatewayinfra.WorkspaceHandle) (gatewayactions.ApprovalWorkflow, error) {
	return s.connectorActionApplication().Approval(s.connectorActionWorkspace(runtime))
}

func (s *Server) connectorActionShutdownWorkflow(runtime *gatewayinfra.WorkspaceHandle) (gatewayactions.ShutdownWorkflow, error) {
	return s.connectorActionApplication().Shutdown(s.connectorActionWorkspace(runtime))
}
