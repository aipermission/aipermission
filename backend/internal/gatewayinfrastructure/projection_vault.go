package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"log"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type VaultRuntimePorts struct {
	InvalidateSessions func(context.Context, []gatewayvault.SessionReference, gatewayvault.SessionMutationScope) error
	SessionEnvironment func(context.Context, int64) (bool, error)
	Connector          gatewayvault.ConnectorPort
}

func (component *VaultOwner) VaultRuntime(handle *WorkspaceHandle, ports VaultRuntimePorts) (gatewayvault.Runtime, bool) {
	owner, ok := component.resolve(handle)
	if !ok || owner.Security.PolicyService() == nil || ports.InvalidateSessions == nil ||
		ports.SessionEnvironment == nil || ports.Connector == nil {
		return gatewayvault.Runtime{}, false
	}
	identity := handle.Identity()
	delivery := owner.Security.VaultDeliveryCoordinator()
	observation := component.owner.ObservationOwner()
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
		Project: gatewayvault.ProjectRuntimePorts{
			InvalidateSessions: ports.InvalidateSessions,
			SessionEnvironment: ports.SessionEnvironment,
			Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
				return observation.WithObservationMutation(ctx, handle, "user", nil, 0, action, payload, mutate)
			},
			Observe: func(ctx context.Context, action string, payload any) error {
				return observation.WriteObservationRequired(ctx, handle, "user", nil, 0, action, payload)
			},
		},
		Action: gatewayvault.ActionRuntimePorts{
			Connector: ports.Connector,
			Mutate: func(ctx context.Context, tokenID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
				return observation.WithObservationMutation(ctx, handle, "mcp", &tokenID, 0, action, payload, mutate)
			},
		},
		Requests: gatewayvault.RequestRuntimePorts{
			Store: observation.VaultRequestStoreFactory(handle),
			Mutate: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
				return observation.WithObservationMutation(ctx, handle, actor, tokenID, runtimeID, action, payload, mutate)
			},
			Observe: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
				observation.WriteObservation(ctx, handle, actor, tokenID, runtimeID, action, payload)
			},
			RepairProjection: func(ctx context.Context, id int64) error {
				if err := observation.SyncVaultActionRequest(ctx, handle, id); err != nil {
					log.Printf("Vault request history projection repair failed request=%d error=%v", id, err)
				}
				return nil
			},
			RedactRequestError: func(ctx context.Context, err error) string {
				if err == nil {
					return ""
				}
				return owner.Security.PolicyService().Redact(ctx, err.Error())
			},
		},
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
		InstallSessionClosed: func(hook func(context.Context, gatewayvault.VaultSessionReference) error) {
			owner.Connectors.ConfigureSessionClosedHook(func(ctx context.Context, sessionID, runtimeID, generation int64) error {
				return hook(ctx, gatewayvault.VaultSessionReference{
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
