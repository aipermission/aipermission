package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewaybackup "github.com/aipermission/aipermission/backend/internal/gatewayoperations/backup"
	gatewaytransfer "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
)

type FileTransferHTTPHandlers interface {
	ListFileTransfers(http.ResponseWriter, *http.Request)
	GetFileTransfer(http.ResponseWriter, *http.Request)
	DownloadTransferredFile(http.ResponseWriter, *http.Request)
	CancelFileTransfer(http.ResponseWriter, *http.Request)
	ListFileTransferBatches(http.ResponseWriter, *http.Request)
	GetFileTransferBatch(http.ResponseWriter, *http.Request)
	DownloadFileTransferBatch(http.ResponseWriter, *http.Request)
	PauseFileTransferBatch(http.ResponseWriter, *http.Request)
	ResumeFileTransferBatch(http.ResponseWriter, *http.Request)
	CancelFileTransferBatch(http.ResponseWriter, *http.Request)
	UpdateFileTransferBatchQueue(http.ResponseWriter, *http.Request)
	ApproveFileTransferBatch(http.ResponseWriter, *http.Request)
	DeclineFileTransferBatch(http.ResponseWriter, *http.Request)
	BrowseRemoteFiles(http.ResponseWriter, *http.Request)
	ExpandRemoteFiles(http.ResponseWriter, *http.Request)
	StartUpload(http.ResponseWriter, *http.Request)
	StartUploadBatch(http.ResponseWriter, *http.Request)
	StartDownload(http.ResponseWriter, *http.Request)
	StartDownloadBatch(http.ResponseWriter, *http.Request)
}

func (component *OperationsOwner) ConfigureFileTransfers(
	scope func(http.ResponseWriter) (*WorkspaceHandle, bool),
	adapterFor gatewaytransfer.FileTransferAdapterProvider,
	dataPath string,
) error {
	if component == nil || component.transfers == nil || scope == nil || adapterFor == nil {
		return InitializationError()
	}
	component.transferHTTP = component.transfers.NewHTTPHandlers(gatewaytransfer.FileTransferHTTPDependencies{
		Scope: func(w http.ResponseWriter) (gatewaytransfer.Workspace, bool) {
			handle, ok := scope(w)
			if !ok {
				return gatewaytransfer.Workspace{}, false
			}
			return component.TransferWorkspace(handle), true
		},
		AdapterFor: adapterFor,
		DataPath:   dataPath,
	})
	return nil
}

func (component *OperationsOwner) FileTransferHTTPHandlers() FileTransferHTTPHandlers {
	if component == nil {
		return nil
	}
	return component.transferHTTP
}

func (component *OperationsOwner) passwordValidationDatabase(handle *WorkspaceHandle) (*sql.DB, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return nil, false
	}
	projected := capabilities.PasswordValidation
	capability, ok := projected.Current()
	if !ok || capability.Database == nil {
		return nil, false
	}
	return capability.Database, true
}

func (component *OperationsOwner) InitializeCommandRuntime(handle *WorkspaceHandle, commands *gatewayoperations.CommandComponent, redact func(context.Context, string) string, timeout time.Duration) error {
	capabilities, available := component.projection(handle)
	if !available || commands == nil {
		return ErrWorkspaceHandleUnavailable
	}
	projected := capabilities.Command
	capability, ok := projected.Current()
	if !ok {
		return ErrWorkspaceHandleUnavailable
	}
	identity := handle.Identity()
	return commands.Initialize(identity.RuntimeID, gatewayoperations.CommandRuntimeDependencies{
		Database: capability.Database, Vault: capability.Vault, WorkspaceID: identity.WorkspaceID,
		Redact: redact, Sessions: capability.Sessions, BackgroundTimeout: timeout,
	})
}

func (component *OperationsOwner) CommandBulkRuntime(handle *WorkspaceHandle, runtime gatewayoperations.CommandBulkHTTPRuntime) (*gatewayoperations.CommandBulkHTTPRuntime, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return nil, false
	}
	projected := capabilities.CommandBulk
	capability, ok := projected.Current()
	if !ok {
		return nil, false
	}
	runtime.Sessions = capability.Sessions
	runtime.WithTransaction = func(ctx context.Context, mutate func(*sql.Tx, gatewayoperations.CommandBulkAuditAppender) error) error {
		return component.observation.withObservationTransaction(ctx, handle, func(tx *sql.Tx, appendAudit observationAppender) error {
			return mutate(tx, gatewayoperations.CommandBulkAuditAppender(appendAudit))
		})
	}
	return &runtime, true
}

func (component *OperationsOwner) LiveConsoleHTTPRuntime(handle *WorkspaceHandle, runtime connectorapi.LiveConsoleHTTPRuntime) (*connectorapi.LiveConsoleHTTPRuntime, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return nil, false
	}
	projected := capabilities.LiveConsole
	capability, ok := projected.Current()
	if !ok {
		return nil, false
	}
	runtime.Sessions = connectorports.NewLiveConsoleSessions(capability.Sessions)
	return &runtime, true
}

func (component *OperationsOwner) backupWorkspace(handle *WorkspaceHandle, runtime gatewaybackup.Runtime) (gatewaybackup.Runtime, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return gatewaybackup.Runtime{}, false
	}
	projected := capabilities.Backup
	capability, ok := projected.Current()
	if !ok {
		return gatewaybackup.Runtime{}, false
	}
	identity := handle.Identity()
	runtime.Database = capability.Database
	runtime.SecretVault = capability.Vault
	runtime.DatabaseID = identity.DatabaseID
	runtime.DatabasePath = identity.DatabasePath
	runtime.WorkspaceID = identity.WorkspaceID
	return runtime, true
}

func (component *OperationsOwner) InitializeTransferWorkspace(
	ctx context.Context,
	handle *WorkspaceHandle,
	observe func(context.Context, string, *int64, int64, string, any),
	resolve FileTransferConnectorPortsResolver,
) error {
	capabilities, available := component.projection(handle)
	if !available || component.transfers == nil || component.transferHTTP == nil {
		return ErrWorkspaceHandleUnavailable
	}
	projected := capabilities.Transfer
	capability, ok := projected.Current()
	if !ok {
		return ErrWorkspaceHandleUnavailable
	}
	return component.transfers.InitializeWorkspace(
		ctx,
		gatewaytransfer.Workspace{RuntimeID: handle.Identity().RuntimeID, StorageID: handle.Identity().UIRetryID},
		capability.Database, observe,
		func(ctx context.Context, runtimeID int64) (gatewaytransfer.FileTransferConnectorPorts, error) {
			ports, err := resolve(ctx, runtimeID)
			return gatewaytransfer.FileTransferConnectorPorts{
				ConnectorKind: ports.ConnectorKind, Gateway: ports.Gateway, Runtime: ports.Runtime,
				CredentialBoundary: ports.CredentialBoundary,
			}, err
		},
	)
}

func (component *OperationsOwner) TransferWorkspace(handle *WorkspaceHandle) gatewaytransfer.Workspace {
	if component == nil || !component.valid(handle) {
		return gatewaytransfer.Workspace{}
	}
	return gatewaytransfer.Workspace{RuntimeID: handle.Identity().RuntimeID, StorageID: handle.Identity().UIRetryID}
}

func (component *OperationsOwner) StopTransferWorkspace(handle *WorkspaceHandle) {
	if component == nil || component.transfers == nil {
		return
	}
	if lifecycle := component.transfers.Lifecycle(component.TransferWorkspace(handle)); lifecycle != nil {
		lifecycle.Abort(context.Background())
	}
}

func (component *OperationsOwner) TransferLifecycle(handle *WorkspaceHandle) TransferWorkflow {
	if component == nil || component.transfers == nil {
		return nil
	}
	return component.transfers.Lifecycle(component.TransferWorkspace(handle))
}

func (component *OperationsOwner) FileTransferJobs(handle *WorkspaceHandle) (gatewaytransfer.Jobs, error) {
	if component == nil || component.transfers == nil {
		return nil, ErrWorkspaceHandleUnavailable
	}
	return component.transfers.WorkspaceJobs(component.TransferWorkspace(handle))
}

func (component *OperationsOwner) LaunchTransferDownload(
	ctx context.Context,
	handle *WorkspaceHandle,
	authorization connectorapi.TransferAuthorization,
	runtimeID int64,
	paths []string,
	archiveName string,
	source string,
) (connectorapi.TransferBatch, error) {
	workspace := component.TransferWorkspace(handle)
	if component == nil || component.transfers == nil || component.transferHTTP == nil || !component.transfers.WorkspaceReady(workspace) {
		return connectorapi.TransferBatch{}, ErrWorkspaceHandleUnavailable
	}
	batch, err := component.transfers.CreateAndLaunchDownloadBatch(
		ctx, workspace, component.transferHTTP, authorization, runtimeID, paths, archiveName, source,
	)
	return connectorapi.TransferBatch{ID: batch.ID, Status: batch.Status, ItemCount: len(batch.Items)}, err
}

func (component *OperationsOwner) PeerTrustWorkspace(handle *WorkspaceHandle, invalidate func(context.Context, string) error) (connectorports.PeerTrustWorkspace, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return connectorports.PeerTrustWorkspace{}, false
	}
	projected := capabilities.PeerTrust
	capability, ok := projected.Current()
	if !ok || capability.Delivery == nil {
		return connectorports.PeerTrustWorkspace{}, false
	}
	return connectorports.PeerTrustWorkspace{
		Identifier:       handle.Identity().DatabaseID,
		AcquireExclusive: capability.Delivery.AcquireExclusive,
		InvalidateAll:    invalidate,
	}, true
}
