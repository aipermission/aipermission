package gatewayinfrastructure

import (
	"context"
	"database/sql"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (component *ConnectorManagementOwner) lifecycleMutationRunner(handle *WorkspaceHandle) connectormgmt.AuditedMutation {
	if _, ok := component.projection(handle); !ok {
		return nil
	}
	return func(ctx context.Context, actor, action string, payload func() any, mutate func(*sql.Tx) error) error {
		return component.observation.withObservationMutation(ctx, handle, actor, nil, 0, action, payload, mutate)
	}
}

func (component *ConnectorManagementOwner) connectorCatalog(handle *WorkspaceHandle, application *connectormgmt.Component) connectormgmt.Catalog {
	capabilities, available := component.projection(handle)
	if !available || application == nil {
		return connectormgmt.Catalog{}
	}
	projected := capabilities.Catalog
	capability, ok := projected.Current()
	if !ok {
		return connectormgmt.Catalog{}
	}
	return application.Catalog(capability.Database, capability.Registry)
}

func (component *ConnectorManagementOwner) connectorCredentialStorage(handle *WorkspaceHandle) connectormgmt.CredentialStorage {
	capabilities, available := component.projection(handle)
	if !available {
		return connectormgmt.CredentialStorage{}
	}
	projected := capabilities.Credential
	capability, ok := projected.Current()
	if !ok {
		return connectormgmt.CredentialStorage{}
	}
	return connectormgmt.CredentialStorage{
		Vault: capability.Vault, WorkspaceID: handle.Identity().WorkspaceID,
	}
}

func (component *ConnectorManagementOwner) connectorManagementWorkspace(handle *WorkspaceHandle, ports connectormgmt.Workspace) (connectormgmt.Workspace, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return connectormgmt.Workspace{}, false
	}
	projected := capabilities.Management
	capability, ok := projected.Current()
	if !ok || capability.Delivery == nil {
		return connectormgmt.Workspace{}, false
	}
	ports.Storage.Database = capability.Database
	ports.Storage.Registry = capability.Registry
	ports.Storage.AcquireExclusive = capability.Delivery.AcquireExclusive
	ports.Storage.Transaction = func(ctx context.Context, mutate func(*sql.Tx, connectormgmt.AuditAppender) error) error {
		return component.observation.withObservationTransaction(ctx, handle, func(tx *sql.Tx, appendAudit observationAppender) error {
			return mutate(tx, connectormgmt.AuditAppender(appendAudit))
		})
	}
	return ports, true
}
