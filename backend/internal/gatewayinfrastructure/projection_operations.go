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

func (component *OperationsOwner) InitializeCommandRuntime(handle *WorkspaceHandle, commands *gatewayoperations.CommandComponent, redact func(context.Context, string) string, timeout time.Duration) error {
	owner, ok := component.resolve(handle)
	if !ok || commands == nil {
		return ErrWorkspaceHandleUnavailable
	}
	identity := handle.Identity()
	return commands.Initialize(identity.RuntimeID, gatewayoperations.CommandRuntimeDependencies{
		Database: owner.Storage.DatabaseHandle(), Vault: owner.Storage.SecretVault(), WorkspaceID: identity.WorkspaceID,
		Redact: redact, Sessions: owner.Connectors.ConsoleSessionManager(), BackgroundTimeout: timeout,
	})
}

func (component *OperationsOwner) CommandBulkRuntime(handle *WorkspaceHandle, runtime gatewayoperations.CommandBulkHTTPRuntime) (*gatewayoperations.CommandBulkHTTPRuntime, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return nil, false
	}
	runtime.Sessions = owner.Connectors.ConsoleSessionManager()
	runtime.WithTransaction = func(ctx context.Context, mutate func(*sql.Tx, gatewayoperations.CommandBulkAuditAppender) error) error {
		return component.owner.withObservationTransaction(ctx, handle, func(tx *sql.Tx, appendAudit ObservationAppender) error {
			return mutate(tx, gatewayoperations.CommandBulkAuditAppender(appendAudit))
		})
	}
	return &runtime, true
}

func (component *OperationsOwner) LiveConsoleHTTPRuntime(handle *WorkspaceHandle, runtime connectorapi.LiveConsoleHTTPRuntime) (*connectorapi.LiveConsoleHTTPRuntime, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return nil, false
	}
	runtime.Sessions = connectorports.NewLiveConsoleSessions(owner.Connectors.ConsoleSessionManager())
	return &runtime, true
}

func (component *OperationsOwner) backupWorkspace(handle *WorkspaceHandle, runtime gatewaybackup.Runtime) (gatewaybackup.Runtime, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return gatewaybackup.Runtime{}, false
	}
	identity := handle.Identity()
	runtime.Database = owner.Storage.DatabaseHandle()
	runtime.SecretVault = owner.Storage.SecretVault()
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
	owner, ok := component.resolve(handle)
	if !ok || transfers == nil {
		return ErrWorkspaceHandleUnavailable
	}
	return transfers.InitializeWorkspace(
		gatewaytransfer.Workspace{RuntimeID: handle.Identity().RuntimeID},
		owner.Storage.DatabaseHandle(), observe, resolve,
	)
}

func (component *OperationsOwner) PeerTrustWorkspace(handle *WorkspaceHandle, invalidate func(context.Context, string) error) (connectorports.PeerTrustWorkspace, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return connectorports.PeerTrustWorkspace{}, false
	}
	return connectorports.PeerTrustWorkspace{
		Identifier:       handle.Identity().DatabaseID,
		AcquireExclusive: owner.Security.VaultDeliveryCoordinator().AcquireExclusive,
		InvalidateAll:    invalidate,
	}, true
}
