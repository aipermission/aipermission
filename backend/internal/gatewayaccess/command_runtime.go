package gatewayaccess

import "github.com/aipermission/aipermission/backend/internal/commandrequests"

type CommandRuntimeDependencies = commandrequests.WorkspaceRuntimeDependencies
type CommandRuntime = commandrequests.Runtime

func (component *Component) InitializeCommandRuntime(runtimeID string, dependencies CommandRuntimeDependencies) error {
	if component == nil {
		return ErrComponentUnavailable
	}
	_, err := component.commandRuntimes.LoadOrCreate(runtimeID, func() (*commandrequests.Runtime, error) {
		return commandrequests.NewWorkspaceRuntime(dependencies)
	})
	return err
}

func (component *Component) CommandRuntime(runtimeID string) (*CommandRuntime, error) {
	if component == nil {
		return nil, ErrComponentUnavailable
	}
	runtime, ok := component.commandRuntimes.Load(runtimeID)
	if !ok || runtime == nil {
		return nil, ErrCommandRuntimeUnavailable
	}
	return runtime, nil
}

func (component *Component) ReleaseCommandRuntime(runtimeID string) {
	if component != nil {
		component.commandRuntimes.Delete(runtimeID)
	}
}
