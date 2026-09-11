package gatewaytransfer

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/runtime"
)

// FileTransferWorkspace is the transfer-owned portion of an unlocked workspace.
type FileTransferWorkspace interface {
	transferapp.Workspace
}

type FileTransferWorkspaceScope func(http.ResponseWriter) (FileTransferWorkspace, bool)

// CredentialBoundary keeps the transfer-owned redaction primitive behind the
// transfer composition contract.
type CredentialBoundary struct {
	value actionresult.CredentialBoundary
}

func NewCredentialBoundary(secrets map[string]any) CredentialBoundary {
	return CredentialBoundary{value: actionresult.NewCredentialBoundary(secrets)}
}

func (boundary CredentialBoundary) Add(values ...string) { boundary.value.Add(values...) }
func (boundary CredentialBoundary) AddStructured(value any) {
	boundary.value.AddStructured(value)
}
func (boundary CredentialBoundary) Valid() bool { return boundary.value.Valid() }

type FileTransferConnectorPorts struct {
	ConnectorKind      string
	Gateway            connectorapi.FileTransferGateway
	Runtime            connectorapi.TransferRuntime
	CredentialBoundary CredentialBoundary
}

type FileTransferConnectorPortsResolver func(context.Context, int64) (FileTransferConnectorPorts, error)

type FileTransferHTTPDependencies struct {
	Scope      FileTransferWorkspaceScope
	AdapterFor FileTransferAdapterProvider
	DataPath   string
}

// Component owns every workspace-scoped transfer runtime for one gateway
// process. Workspace identity is only used as an opaque index key.
type Component struct {
	runtimes transferapp.Manager
}

func NewComponent() *Component { return &Component{} }

func (component *Component) InitializeWorkspace(
	workspace FileTransferWorkspace,
	database *sql.DB,
	observe transferapp.ObservationAudit,
	resolve FileTransferConnectorPortsResolver,
) error {
	if workspace == nil {
		return fmt.Errorf("file transfer workspace state is unavailable")
	}
	if component == nil {
		return fmt.Errorf("file transfer component is unavailable")
	}
	if resolve == nil {
		return fmt.Errorf("file transfer connector resolver is unavailable")
	}
	return component.runtimes.InitializeWorkspace(workspace, database, observe, func(ctx context.Context, runtimeID int64) (transferapp.ConnectorPorts, error) {
		ports, err := resolve(ctx, runtimeID)
		if err != nil {
			return transferapp.ConnectorPorts{}, err
		}
		return transferapp.ConnectorPorts{
			ConnectorKind: ports.ConnectorKind, Gateway: ports.Gateway, Runtime: ports.Runtime,
			CredentialBoundary: ports.CredentialBoundary.value,
		}, nil
	})
}

func (component *Component) NewHTTPHandlers(dependencies FileTransferHTTPDependencies) *FileTransferHTTPHandlers {
	return newFileTransferHTTPHandlers(fileTransferHandlerDependencies{
		Scope: func(w http.ResponseWriter) (*transferapp.Runtime, bool) {
			if dependencies.Scope == nil {
				writeInternalError(w)
				return nil, false
			}
			workspace, ok := dependencies.Scope(w)
			if !ok {
				return nil, false
			}
			if workspace == nil {
				writeInternalError(w)
				return nil, false
			}
			if component == nil {
				writeInternalError(w)
				return nil, false
			}
			runtime, err := component.runtimes.RuntimeForWorkspace(workspace)
			if err != nil {
				writeInternalError(w)
				return nil, false
			}
			return runtime, true
		},
		AdapterFor: dependencies.AdapterFor, DataPath: dependencies.DataPath,
	})
}

func (component *Component) CreateAndLaunchDownloadBatch(
	ctx context.Context,
	workspace FileTransferWorkspace,
	handlers *FileTransferHTTPHandlers,
	authorization connectorapi.TransferAuthorization,
	runtimeID int64,
	remotePaths []string,
	archiveName string,
	source string,
) (filetransfer.BatchRecord, error) {
	if workspace == nil {
		return filetransfer.BatchRecord{}, fmt.Errorf("file transfer workspace runtime is unavailable")
	}
	if component == nil || handlers == nil {
		return filetransfer.BatchRecord{}, fmt.Errorf("file transfer component is unavailable")
	}
	runtime, err := component.runtimes.RuntimeForWorkspace(workspace)
	if err != nil {
		return filetransfer.BatchRecord{}, err
	}
	return handlers.CreateAndLaunchDownloadBatch(ctx, runtime, authorization, runtimeID, remotePaths, archiveName, source)
}

func (component *Component) WorkspaceReady(workspace FileTransferWorkspace) bool {
	return component != nil && workspace != nil && component.runtimes.WorkspaceReady(workspace)
}

func (component *Component) WorkspaceJobs(workspace FileTransferWorkspace) (transferapp.Jobs, error) {
	if component == nil {
		return nil, fmt.Errorf("file transfer component is unavailable")
	}
	return component.runtimes.WorkspaceJobs(workspace)
}

type WorkspaceLifecycle interface {
	Shutdown(time.Duration, string, string) (bool, bool, error)
	Wait(context.Context) bool
	Abort()
}

type workspaceLifecycle struct {
	manager   *transferapp.Manager
	workspace FileTransferWorkspace
}

func (component *Component) Lifecycle(workspace FileTransferWorkspace) WorkspaceLifecycle {
	if component == nil || workspace == nil {
		return nil
	}
	return workspaceLifecycle{manager: &component.runtimes, workspace: workspace}
}

func (lifecycle workspaceLifecycle) Shutdown(timeout time.Duration, runningMessage, batchMessage string) (bool, bool, error) {
	return lifecycle.manager.ShutdownWorkspace(lifecycle.workspace, timeout, runningMessage, batchMessage)
}

func (lifecycle workspaceLifecycle) Wait(ctx context.Context) bool {
	return lifecycle.manager.WaitWorkspace(ctx, lifecycle.workspace)
}

func (lifecycle workspaceLifecycle) Abort() {
	lifecycle.manager.RemoveWorkspace(lifecycle.workspace)
}
