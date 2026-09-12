package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"time"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewaybackup "github.com/aipermission/aipermission/backend/internal/gatewayoperations/backup"
	gatewaytransfer "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
)

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
	handle *WorkspaceHandle,
	transfers *gatewaytransfer.Component,
	observe func(context.Context, string, *int64, int64, string, any),
	resolve gatewaytransfer.FileTransferConnectorPortsResolver,
) error {
	capabilities, available := component.projection(handle)
	if !available || transfers == nil {
		return ErrWorkspaceHandleUnavailable
	}
	projected := capabilities.Transfer
	capability, ok := projected.Current()
	if !ok {
		return ErrWorkspaceHandleUnavailable
	}
	return transfers.InitializeWorkspace(
		gatewaytransfer.Workspace{RuntimeID: handle.Identity().RuntimeID},
		capability.Database, observe, resolve,
	)
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
