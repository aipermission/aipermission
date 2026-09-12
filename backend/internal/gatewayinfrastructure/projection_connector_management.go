package gatewayinfrastructure

import connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"

func (component *Component) ConnectorCatalog(handle *WorkspaceHandle, application *connectormgmt.Component) connectormgmt.Catalog {
	owner, ok := component.resolve(handle)
	if !ok || application == nil {
		return connectormgmt.Catalog{}
	}
	return application.Catalog(owner.Storage.DatabaseHandle(), owner.Connectors.ConnectorRegistry())
}

func (component *Component) ConnectorCredentialStorage(handle *WorkspaceHandle) (connectormgmt.CredentialStorage, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return connectormgmt.CredentialStorage{}, false
	}
	return connectormgmt.CredentialStorage{
		Vault: owner.Storage.SecretVault(), WorkspaceID: handle.Identity().WorkspaceID,
	}, true
}

func (component *Component) ConnectorManagementWorkspace(handle *WorkspaceHandle, ports connectormgmt.Workspace) (connectormgmt.Workspace, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return connectormgmt.Workspace{}, false
	}
	ports.Storage.Database = owner.Storage.DatabaseHandle()
	ports.Storage.Registry = owner.Connectors.ConnectorRegistry()
	ports.Storage.AcquireExclusive = owner.Security.VaultDeliveryCoordinator().AcquireExclusive
	return ports, true
}
