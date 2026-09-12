package gatewayworkspace

import connectorcapability "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/capabilities/connector"

func (runtime *Runtime) ConnectorActionCapability() (connectorcapability.ActionCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return connectorcapability.ActionCapability{}, false
	}
	return connectorcapability.NewAction(
		owner.Storage.DatabaseHandle(), owner.Storage.TokenStore(), owner.Connectors.ConnectorRegistry(),
		owner.Storage.SecretVault(), owner.Security.PolicyService(), owner.Security.VaultDeliveryCoordinator(),
		owner.Security.RuntimeControlState(),
	), true
}

func (runtime *Runtime) ConnectorApprovalCapability() (connectorcapability.ApprovalCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return connectorcapability.ApprovalCapability{}, false
	}
	return connectorcapability.NewApproval(owner.Storage.DatabaseHandle(), owner.Security.RuntimeControlState()), true
}

func (runtime *Runtime) ConnectorCatalogCapability() (connectorcapability.CatalogCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return connectorcapability.CatalogCapability{}, false
	}
	return connectorcapability.NewCatalog(owner.Storage.DatabaseHandle(), owner.Connectors.ConnectorRegistry()), true
}

func (runtime *Runtime) ConnectorCredentialCapability() (connectorcapability.CredentialCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return connectorcapability.CredentialCapability{}, false
	}
	return connectorcapability.NewCredential(owner.Storage.SecretVault()), true
}

func (runtime *Runtime) ConnectorManagementCapability() (connectorcapability.ManagementCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return connectorcapability.ManagementCapability{}, false
	}
	return connectorcapability.NewManagement(
		owner.Storage.DatabaseHandle(), owner.Connectors.ConnectorRegistry(), owner.Security.VaultDeliveryCoordinator(),
	), true
}

func (runtime *Runtime) ConnectorTransportCapability() (connectorcapability.TransportCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return connectorcapability.TransportCapability{}, false
	}
	return connectorcapability.NewTransport(
		&owner.Connectors, owner.Storage.DatabaseHandle(), owner.Security.VaultDeliveryCoordinator(),
	), true
}
