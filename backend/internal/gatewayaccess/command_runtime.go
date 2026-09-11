package gatewayaccess

import "github.com/aipermission/aipermission/backend/internal/commandrequests"

type CommandRuntimeDependencies = commandrequests.WorkspaceRuntimeDependencies
type CommandRuntime = commandrequests.Runtime

func (component *Component) InitializeCommandRuntime(workspace commandrequests.Workspace, dependencies CommandRuntimeDependencies) error {
	if component == nil {
		return ErrComponentUnavailable
	}
	return commandrequests.InitializeWorkspace(workspace, dependencies)
}

func (component *Component) CommandRuntime(workspace commandrequests.Workspace) (*CommandRuntime, error) {
	if component == nil {
		return nil, ErrComponentUnavailable
	}
	return commandrequests.RuntimeForWorkspace(workspace)
}
