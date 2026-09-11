package transferruntime

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/runtimeindex"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

type Workspace interface {
	RuntimeIdentifier() string
}

type workspaceRuntimeHandle struct {
	lifecycle *Lifecycle
	runtime   *Runtime
}

type Manager struct {
	runtimes runtimeindex.Index[*workspaceRuntimeHandle]
}

func (manager *Manager) InitializeWorkspace(workspace Workspace, database *sql.DB, observe ObservationAudit, resolve ConnectorPortsResolver) error {
	id, err := workspaceID(workspace)
	if err != nil {
		return err
	}
	_, err = manager.runtimes.LoadOrCreate(id, func() (*workspaceRuntimeHandle, error) {
		lifecycle := NewLifecycle()
		runtime, err := lifecycle.NewRuntime(database, observe, resolve)
		if err != nil {
			lifecycle.Stop()
			return nil, err
		}
		return &workspaceRuntimeHandle{lifecycle: lifecycle, runtime: runtime}, nil
	})
	return err
}

func (manager *Manager) RuntimeForWorkspace(workspace Workspace) (*Runtime, error) {
	handle, ok, err := manager.loadWorkspaceHandle(workspace)
	if err != nil {
		return nil, err
	}
	if !ok || handle == nil || handle.runtime == nil {
		return nil, fmt.Errorf("file transfer workspace runtime is unavailable")
	}
	return handle.runtime, nil
}

func (manager *Manager) WorkspaceReady(workspace Workspace) bool {
	_, err := manager.RuntimeForWorkspace(workspace)
	return err == nil
}

func (manager *Manager) ShutdownWorkspace(workspace Workspace, timeout time.Duration, runningMessage, batchMessage string) (bool, bool, error) {
	id, err := workspaceID(workspace)
	if err != nil {
		return false, true, err
	}
	handle, ok := manager.runtimes.Load(id)
	if !ok || handle == nil || handle.runtime == nil {
		manager.StopWorkspace(workspace)
		return false, true, nil
	}
	drained, shutdownErr := handle.runtime.Shutdown(timeout, runningMessage, batchMessage)
	if drained {
		manager.runtimes.Delete(id)
	}
	return true, drained, shutdownErr
}

func (manager *Manager) StopWorkspace(workspace Workspace) {
	handle, ok, err := manager.loadWorkspaceHandle(workspace)
	if err == nil && ok && handle != nil && handle.lifecycle != nil {
		handle.lifecycle.Stop()
	}
}

func (manager *Manager) RemoveWorkspace(workspace Workspace) {
	id, err := workspaceID(workspace)
	if err != nil {
		return
	}
	if handle, ok := manager.runtimes.Delete(id); ok && handle != nil && handle.lifecycle != nil {
		handle.lifecycle.Stop()
	}
}

func (manager *Manager) WaitWorkspace(ctx context.Context, workspace Workspace) bool {
	id, err := workspaceID(workspace)
	if err != nil {
		return true
	}
	handle, ok := manager.runtimes.Load(id)
	if !ok || handle == nil || handle.lifecycle == nil {
		return true
	}
	drained := handle.lifecycle.Wait(ctx)
	if drained {
		manager.runtimes.Delete(id)
	}
	return drained
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

func (manager *Manager) WorkspaceJobs(workspace Workspace) (Jobs, error) {
	handle, ok, err := manager.loadWorkspaceHandle(workspace)
	if err != nil {
		return nil, err
	}
	if !ok || handle == nil || handle.lifecycle == nil || handle.lifecycle.Registry() == nil {
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

func (manager *Manager) loadWorkspaceHandle(workspace Workspace) (*workspaceRuntimeHandle, bool, error) {
	id, err := workspaceID(workspace)
	if err != nil {
		return nil, false, err
	}
	handle, ok := manager.runtimes.Load(id)
	return handle, ok, nil
}

func workspaceID(workspace Workspace) (string, error) {
	if workspace == nil {
		return "", fmt.Errorf("file transfer workspace state is unavailable")
	}
	id := strings.TrimSpace(workspace.RuntimeIdentifier())
	if id == "" {
		return "", fmt.Errorf("file transfer workspace identity is unavailable")
	}
	return id, nil
}
