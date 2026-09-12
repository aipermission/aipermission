package gatewayinfrastructure

import (
	"context"
	"database/sql"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (component *ConnectorManagementOwner) lifecycleMutationRunner(handle *WorkspaceHandle) connectormgmt.AuditedMutation {
	if _, ok := component.resolve(handle); !ok {
		return nil
	}
	return func(ctx context.Context, actor, action string, payload func() any, mutate func(*sql.Tx) error) error {
		return component.owner.withObservationMutation(ctx, handle, actor, nil, 0, action, payload, mutate)
	}
}

func (component *ConnectorManagementOwner) connectorCatalog(handle *WorkspaceHandle, application *connectormgmt.Component) connectormgmt.Catalog {
	owner, ok := component.resolve(handle)
	if !ok || application == nil {
		return connectormgmt.Catalog{}
	}
	return application.Catalog(owner.Storage.DatabaseHandle(), owner.Connectors.ConnectorRegistry())
}

func (component *ConnectorManagementOwner) connectorCredentialStorage(handle *WorkspaceHandle) connectormgmt.CredentialStorage {
	owner, ok := component.resolve(handle)
	if !ok {
		return connectormgmt.CredentialStorage{}
	}
	return connectormgmt.CredentialStorage{
		Vault: owner.Storage.SecretVault(), WorkspaceID: handle.Identity().WorkspaceID,
	}
}

func (component *ConnectorManagementOwner) connectorManagementWorkspace(handle *WorkspaceHandle, ports connectormgmt.Workspace) (connectormgmt.Workspace, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return connectormgmt.Workspace{}, false
	}
	ports.Storage.Database = owner.Storage.DatabaseHandle()
	ports.Storage.Registry = owner.Connectors.ConnectorRegistry()
	ports.Storage.AcquireExclusive = owner.Security.VaultDeliveryCoordinator().AcquireExclusive
	ports.Storage.Transaction = func(ctx context.Context, mutate func(*sql.Tx, connectormgmt.AuditAppender) error) error {
		return component.owner.withObservationTransaction(ctx, handle, func(tx *sql.Tx, appendAudit ObservationAppender) error {
			return mutate(tx, connectormgmt.AuditAppender(appendAudit))
		})
	}
	return ports, true
}
