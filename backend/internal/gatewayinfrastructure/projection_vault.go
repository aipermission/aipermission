package gatewayinfrastructure

import (
	"context"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type VaultRuntimePorts struct {
	Project  gatewayvault.ProjectRuntimePorts
	Action   gatewayvault.ActionRuntimePorts
	Requests gatewayvault.RequestRuntimePorts
}

func (component *VaultOwner) VaultRuntime(handle *WorkspaceHandle, ports VaultRuntimePorts) (gatewayvault.Runtime, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return gatewayvault.Runtime{}, false
	}
	identity := handle.Identity()
	delivery := owner.Security.VaultDeliveryCoordinator()
	return gatewayvault.Runtime{
		Storage: gatewayvault.StorageRuntime{
			Database: owner.Storage.DatabaseHandle(), SecretVault: owner.Storage.SecretVault(),
			Tokens: owner.Storage.TokenStore(), WorkspaceID: identity.WorkspaceID, DatabaseID: identity.DatabaseID,
		},
		Session: gatewayvault.SessionRuntime{
			Sessions: owner.Connectors.ConsoleSessionManager(), Leases: owner.Security.VaultLeaseStore(),
			RuntimeInstanceID: identity.RuntimeID, MCPStarted: owner.Security.RuntimeControlState().MCPStarted,
			AcquireDelivery: delivery.AcquireDelivery, AcquireExclusive: delivery.AcquireExclusive,
		},
		Project: ports.Project, Action: ports.Action, Requests: ports.Requests,
	}, true
}

type VaultSessionPorts struct {
	Principal func() (gatewayaccess.Principal, error)
	Requests  func(context.Context) (gatewayvault.RequestInvalidator, error)
}

func (component *VaultOwner) VaultSessionRuntime(handle *WorkspaceHandle, ports VaultSessionPorts) (gatewayvault.SessionLifecycleRuntime, bool) {
	owner, ok := component.resolve(handle)
	if !ok || owner.Connectors.ConsoleSessionManager() == nil {
		return gatewayvault.SessionLifecycleRuntime{}, false
	}
	sessions := owner.Connectors.ConsoleSessionManager()
	leases := owner.Security.VaultLeaseStore()
	delivery := owner.Security.VaultDeliveryCoordinator()
	return gatewayvault.SessionLifecycleRuntime{
		Database: owner.Storage.DatabaseHandle(), Leases: leases, Sessions: sessions,
		Principal: ports.Principal, Requests: ports.Requests, AcquireDelivery: delivery.AcquireDelivery,
		InstallAuthorizer: func(guard gatewayvault.SessionAuthorizationGuard) {
			owner.Connectors.ConfigureVaultSessionAuthorizer(leases, guard)
		},
		InstallSessionClosed: func(hook func(gatewayvault.VaultSessionReference)) {
			owner.Connectors.ConfigureSessionClosedHook(func(sessionID, runtimeID, generation int64) {
				hook(gatewayvault.VaultSessionReference{
					SessionID: sessionID, RuntimeID: runtimeID, Generation: generation,
				})
			})
		},
	}, true
}

type VaultMCPPorts struct {
	TokenID      int64
	Runtime      func(context.Context) (gatewayvault.VaultRequestApplication, error)
	MetadataRead func(context.Context, int64) (bool, error)
}

func (component *VaultOwner) VaultMCPScope(handle *WorkspaceHandle, ports VaultMCPPorts) (gatewayvault.VaultMCPHTTPScope, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return gatewayvault.VaultMCPHTTPScope{}, false
	}
	return gatewayvault.VaultMCPHTTPScope{
		Database: owner.Storage.DatabaseHandle(), Vault: owner.Storage.SecretVault(),
		WorkspaceUUID: handle.Identity().WorkspaceID, TokenID: ports.TokenID,
		MCPStarted: owner.Security.RuntimeControlState().MCPStarted,
		Runtime:    ports.Runtime, MetadataRead: ports.MetadataRead,
	}, true
}

func (component *VaultOwner) VaultApprovalScope(handle *WorkspaceHandle, runtime func(context.Context) (gatewayvault.VaultRequestApplication, error)) (gatewayvault.VaultApprovalHTTPScope, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return gatewayvault.VaultApprovalHTTPScope{}, false
	}
	return gatewayvault.VaultApprovalHTTPScope{
		MCPStarted: owner.Security.RuntimeControlState().MCPStarted, Runtime: runtime,
	}, true
}
