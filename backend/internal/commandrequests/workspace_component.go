package commandrequests

import (
	"github.com/aipermission/aipermission/backend/internal/componentstate"
)

var workspaceRuntimeStateKey = componentstate.NewKey[*workspaceRuntimeHandle]("command-request-runtime")

type Workspace interface {
	ComponentStatePort() componentstate.Port
}

type workspaceRuntimeHandle struct {
	runtime *Runtime
}

func InitializeWorkspace(workspace Workspace, dependencies WorkspaceRuntimeDependencies) error {
	if workspace == nil {
		return ErrRuntimeUnavailable
	}
	_, err := componentstate.LoadOrCreate(workspace.ComponentStatePort(), workspaceRuntimeStateKey, func() (*workspaceRuntimeHandle, error) {
		runtime, err := NewWorkspaceRuntime(dependencies)
		if err != nil {
			return nil, err
		}
		return &workspaceRuntimeHandle{runtime: runtime}, nil
	})
	return err
}

func RuntimeForWorkspace(workspace Workspace) (*Runtime, error) {
	if workspace == nil {
		return nil, ErrRuntimeUnavailable
	}
	handle, ok, err := componentstate.Load[*workspaceRuntimeHandle](workspace.ComponentStatePort(), workspaceRuntimeStateKey)
	if err != nil {
		return nil, err
	}
	if !ok || handle == nil || handle.runtime == nil {
		return nil, ErrRuntimeUnavailable
	}
	return handle.runtime, nil
}
