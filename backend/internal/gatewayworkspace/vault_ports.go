package gatewayworkspace

import vaultcapability "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/capabilities/vault"

func (runtime *Runtime) VaultRuntimeCapability() (vaultcapability.RuntimeCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok || owner.Security.PolicyService() == nil {
		return vaultcapability.RuntimeCapability{}, false
	}
	return vaultcapability.NewRuntime(
		owner.Storage.DatabaseHandle(), owner.Storage.SecretVault(), owner.Storage.TokenStore(),
		owner.Connectors.ConsoleSessionManager(), owner.Security.VaultLeaseStore(),
		owner.Security.VaultDeliveryCoordinator(), owner.Security.RuntimeControlState(), owner.Security.PolicyService(),
	), true
}

func (runtime *Runtime) VaultSessionCapability() (vaultcapability.SessionCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok || owner.Connectors.ConsoleSessionManager() == nil {
		return vaultcapability.SessionCapability{}, false
	}
	return vaultcapability.NewSession(
		owner.Storage.DatabaseHandle(), owner.Connectors.ConsoleSessionManager(), owner.Security.VaultLeaseStore(),
		owner.Security.VaultDeliveryCoordinator(),
		owner.Connectors.ConfigureVaultSessionAuthorizer,
		owner.Connectors.ConfigureSessionClosedHook,
	), true
}

func (runtime *Runtime) VaultMCPCapability() (vaultcapability.MCPCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return vaultcapability.MCPCapability{}, false
	}
	return vaultcapability.NewMCP(owner.Storage.DatabaseHandle(), owner.Storage.SecretVault(), owner.Security.RuntimeControlState()), true
}

func (runtime *Runtime) VaultApprovalCapability() (vaultcapability.ApprovalCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return vaultcapability.ApprovalCapability{}, false
	}
	return vaultcapability.NewApproval(owner.Security.RuntimeControlState()), true
}
