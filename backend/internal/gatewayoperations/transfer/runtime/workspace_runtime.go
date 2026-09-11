package transferruntime

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/componentstate"
	"github.com/aipermission/aipermission/backend/internal/runtimeoutcome"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

var workspaceRuntimeStateKey = componentstate.NewKey[*workspaceRuntimeHandle]("file-transfer-runtime")

type Workspace interface {
	ComponentStatePort() componentstate.Port
}

type workspaceRuntimeHandle struct {
	lifecycle *Lifecycle
	runtime   *Runtime
}

func InitializeWorkspace(
	workspace Workspace,
	database *sql.DB,
	observe ObservationAudit,
	resolve ConnectorPortsResolver,
) error {
	state, err := workspaceState(workspace)
	if err != nil {
		return err
	}
	handle, err := componentstate.LoadOrCreate(state, workspaceRuntimeStateKey, func() (*workspaceRuntimeHandle, error) {
		lifecycle := NewLifecycle()
		runtime, err := lifecycle.NewRuntime(database, observe, resolve)
		if err != nil {
			lifecycle.Stop()
			return nil, err
		}
		return &workspaceRuntimeHandle{lifecycle: lifecycle, runtime: runtime}, nil
	})
	if err != nil {
		return err
	}
	return componentstate.RegisterLifecycle(state, workspaceRuntimeStateKey, componentstate.Lifecycle{
		Name: "file-transfer",
		Close: func(ctx context.Context) (bool, error) {
			return handle.runtime.ShutdownContext(ctx, runtimeoutcome.TransferInterrupted, runtimeoutcome.TransferQueueStopped)
		},
		Abort: handle.lifecycle.Stop,
		Wait:  handle.lifecycle.Wait,
	})
}

func RuntimeForWorkspace(workspace Workspace) (*Runtime, error) {
	state, err := workspaceState(workspace)
	if err != nil {
		return nil, err
	}
	handle, ok, err := componentstate.Load[*workspaceRuntimeHandle](state, workspaceRuntimeStateKey)
	if err != nil {
		return nil, err
	}
	if !ok || handle == nil || handle.runtime == nil {
		return nil, fmt.Errorf("file transfer workspace runtime is unavailable")
	}
	return handle.runtime, nil
}

func WorkspaceReady(workspace Workspace) bool {
	_, err := RuntimeForWorkspace(workspace)
	return err == nil
}

func ShutdownWorkspace(workspace Workspace, timeout time.Duration, runningMessage, batchMessage string) (bool, bool, error) {
	state, stateErr := workspaceState(workspace)
	if stateErr != nil {
		return false, true, stateErr
	}
	handle, ok, err := loadWorkspaceHandle(state)
	if err != nil || !ok || handle.runtime == nil {
		stopWorkspaceState(state)
		return false, true, err
	}
	drained, shutdownErr := handle.runtime.Shutdown(timeout, runningMessage, batchMessage)
	return true, drained, shutdownErr
}

func StopWorkspace(workspace Workspace) {
	state, err := workspaceState(workspace)
	if err != nil {
		return
	}
	stopWorkspaceState(state)
}

func stopWorkspaceState(state componentstate.Port) {
	handle, ok, err := loadWorkspaceHandle(state)
	if err == nil && ok && handle.lifecycle != nil {
		handle.lifecycle.Stop()
	}
}

func WaitWorkspace(ctx context.Context, workspace Workspace) bool {
	state, err := workspaceState(workspace)
	if err != nil {
		return true
	}
	handle, ok, err := loadWorkspaceHandle(state)
	return err != nil || !ok || handle.lifecycle == nil || handle.lifecycle.Wait(ctx)
}

type Jobs interface {
	Wait(context.Context) bool
	LaunchFile(int64, context.CancelFunc, func()) bool
	RegisterFileCancel(int64, context.CancelFunc)
	UnregisterFileCancel(int64)
	RegisterBatchCancel(int64, context.CancelFunc)
	UnregisterBatchCancel(int64)
	RegisterBatchControl(int64, *transferjobs.Control)
}

type workspaceJobs struct{ registry *transferjobs.Registry }

func WorkspaceJobs(workspace Workspace) (Jobs, error) {
	state, err := workspaceState(workspace)
	if err != nil {
		return nil, err
	}
	handle, ok, err := loadWorkspaceHandle(state)
	if err != nil {
		return nil, err
	}
	if !ok || handle.lifecycle == nil || handle.lifecycle.Registry() == nil {
		return nil, fmt.Errorf("file transfer workspace jobs are unavailable")
	}
	return workspaceJobs{registry: handle.lifecycle.Registry()}, nil
}

func (jobs workspaceJobs) Wait(ctx context.Context) bool { return jobs.registry.Wait(ctx) }
func (jobs workspaceJobs) LaunchFile(id int64, cancel context.CancelFunc, run func()) bool {
	return jobs.registry.Files.Launch(id, cancel, run)
}
func (jobs workspaceJobs) RegisterFileCancel(id int64, cancel context.CancelFunc) {
	jobs.registry.Files.RegisterCancel(id, cancel)
}
func (jobs workspaceJobs) UnregisterFileCancel(id int64) { jobs.registry.Files.UnregisterCancel(id) }
func (jobs workspaceJobs) RegisterBatchCancel(id int64, cancel context.CancelFunc) {
	jobs.registry.Batches.RegisterCancel(id, cancel)
}
func (jobs workspaceJobs) UnregisterBatchCancel(id int64) { jobs.registry.Batches.UnregisterCancel(id) }
func (jobs workspaceJobs) RegisterBatchControl(id int64, control *transferjobs.Control) {
	jobs.registry.Batches.RegisterControl(id, control)
}

func loadWorkspaceHandle(state componentstate.Port) (*workspaceRuntimeHandle, bool, error) {
	if state == nil {
		return nil, false, nil
	}
	return componentstate.Load[*workspaceRuntimeHandle](state, workspaceRuntimeStateKey)
}

func workspaceState(workspace Workspace) (componentstate.Port, error) {
	if workspace == nil || workspace.ComponentStatePort() == nil {
		return nil, componentstate.ErrUnavailable
	}
	return workspace.ComponentStatePort(), nil
}
