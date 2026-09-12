package gatewayworkspace

import accesscapability "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/capabilities/access"

func (runtime *Runtime) VaultMetadataCapability() (accesscapability.VaultMetadataCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return accesscapability.VaultMetadataCapability{}, false
	}
	return accesscapability.NewVaultMetadata(owner.Storage.DatabaseHandle()), true
}

func (runtime *Runtime) AccessControlCapability() (accesscapability.AccessControlCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return accesscapability.AccessControlCapability{}, false
	}
	return accesscapability.NewAccessControl(
		owner.Storage.DatabaseHandle(), owner.Storage.TokenStore(), owner.Connectors.ConnectorRegistry(),
		owner.Security.PolicyService(), owner.Security.VaultDeliveryCoordinator(),
	), true
}

func (runtime *Runtime) MCPTokenSourceCapability() (accesscapability.MCPTokenSource, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok || owner.Storage.TokenStore() == nil {
		return nil, false
	}
	return owner.Storage.TokenStore(), true
}

func (runtime *Runtime) MCPReadCapability() (accesscapability.MCPReadCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return accesscapability.MCPReadCapability{}, false
	}
	return accesscapability.NewMCPRead(owner.Storage.DatabaseHandle(), owner.Connectors.ConnectorRegistry()), true
}

func (runtime *Runtime) MCPActionCapability() (accesscapability.MCPActionCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return accesscapability.MCPActionCapability{}, false
	}
	return accesscapability.NewMCPAction(
		owner.Storage.DatabaseHandle(), owner.Storage.TokenStore(), owner.Security.VaultLeaseStore(),
		owner.Security.VaultDeliveryCoordinator(), owner.Security.RuntimeControlState(),
	), true
}

func (runtime *Runtime) MCPRuntimeCapability() (accesscapability.MCPRuntimeCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return accesscapability.MCPRuntimeCapability{}, false
	}
	return accesscapability.NewMCPRuntime(owner.Security.RuntimeControlState(), owner.Security.VaultDeliveryCoordinator()), true
}

func (runtime *Runtime) RuntimeControlCapability() (accesscapability.RuntimeControlCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok || owner.Security.RuntimeControlState() == nil {
		return accesscapability.RuntimeControlCapability{}, false
	}
	return accesscapability.NewRuntimeControl(owner.Security.RuntimeControlState()), true
}

func (runtime *Runtime) ConsoleRecoveryCapability() (accesscapability.ConsoleRecoveryCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok || owner.Connectors.ConsoleSessionManager() == nil {
		return accesscapability.ConsoleRecoveryCapability{}, false
	}
	return accesscapability.NewConsoleRecovery(owner.Connectors.ConsoleSessionManager()), true
}

func (runtime *Runtime) SecurityPolicyCapability() (accesscapability.SecurityPolicyCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok || owner.Security.PolicyService() == nil {
		return accesscapability.SecurityPolicyCapability{}, false
	}
	return accesscapability.NewSecurityPolicy(owner.Security.PolicyService()), true
}

func (runtime *Runtime) RuntimeConfigurationCapability() (accesscapability.RuntimeConfigurationCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok || owner.Security.PolicyService() == nil {
		return accesscapability.RuntimeConfigurationCapability{}, false
	}
	return accesscapability.NewRuntimeConfiguration(
		owner.Security.PolicyService(), owner.Security.RuntimeControlState(), owner.Connectors.ConfigureConsoleSessions,
	), true
}

func (runtime *Runtime) ConsoleConfigurationCapability() (accesscapability.ConsoleConfigurationCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return accesscapability.ConsoleConfigurationCapability{}, false
	}
	return accesscapability.NewConsoleConfiguration(owner.Connectors.ConfigureConsoleSessions), true
}

func (runtime *Runtime) MessageCapability() (accesscapability.MessageCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return accesscapability.MessageCapability{}, false
	}
	return accesscapability.NewMessage(owner.Storage.DatabaseHandle()), true
}

func (runtime *Runtime) ProjectCapability() (accesscapability.ProjectCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return accesscapability.ProjectCapability{}, false
	}
	return accesscapability.NewProject(owner.Storage.DatabaseHandle(), owner.Security.VaultDeliveryCoordinator()), true
}
