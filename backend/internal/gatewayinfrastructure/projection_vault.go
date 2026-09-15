package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"log"
	"time"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type VaultRuntimePorts struct {
	InvalidateSessions func(context.Context, []gatewayvault.SessionReference, gatewayvault.SessionMutationScope) error
	SessionEnvironment func(context.Context, int64) (bool, error)
	Connector          gatewayvault.ConnectorPort
}

func (component *VaultOwner) vaultRuntime(handle *WorkspaceHandle, ports VaultRuntimePorts) (gatewayvault.Runtime, bool) {
	capabilities, available := component.projection(handle)
	if !available || ports.InvalidateSessions == nil ||
		ports.SessionEnvironment == nil || ports.Connector == nil {
		return gatewayvault.Runtime{}, false
	}
	projected := capabilities.Runtime
	capability, ok := projected.Current()
	if !ok || capability.Delivery == nil || capability.Control == nil {
		return gatewayvault.Runtime{}, false
	}
	identity := handle.Identity()
	observation := component.observation
	return gatewayvault.Runtime{
		Storage: gatewayvault.StorageRuntime{
			Database: capability.Database, SecretVault: capability.Vault,
			ReadToken: func(ctx context.Context, id int64) (gatewayvault.TokenState, error) {
				token, err := capability.Tokens.Get(ctx, id)
				return gatewayvault.TokenState{
					Active: token.ActiveAt(time.Now().UTC()), ExpiresAt: token.ExpiresAt, UpdatedAt: token.UpdatedAt,
				}, err
			},
			WorkspaceID: identity.WorkspaceID, DatabaseID: identity.DatabaseID,
		},
		Session: gatewayvault.SessionRuntime{
			Sessions: capability.Sessions, Leases: capability.Leases,
			RuntimeInstanceID: identity.RuntimeID, MCPStarted: capability.Control.MCPStarted,
			AcquireDelivery: capability.Delivery.AcquireDelivery, AcquireExclusive: capability.Delivery.AcquireExclusive,
		},
		Project: gatewayvault.ProjectRuntimePorts{
			InvalidateSessions: ports.InvalidateSessions,
			SessionEnvironment: ports.SessionEnvironment,
			Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
				return observation.withObservationMutation(ctx, handle, "user", nil, 0, action, payload, mutate)
			},
			Observe: func(ctx context.Context, action string, payload any) error {
				return observation.writeObservationRequired(ctx, handle, "user", nil, 0, action, payload)
			},
		},
		Action: gatewayvault.ActionRuntimePorts{
			Connector: ports.Connector,
		},
		Requests: gatewayvault.RequestRuntimePorts{
			Store: observation.vaultRequestStoreFactory(handle),
			Transaction: func(ctx context.Context, mutate func(*sql.Tx, gatewayvault.RequestObservationAppender) error) error {
				return observation.withObservationTransaction(ctx, handle, func(tx *sql.Tx, appendObservation observationAppender) error {
					return mutate(tx, gatewayvault.RequestObservationAppender(appendObservation))
				})
			},
			Mutate: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
				return observation.withObservationMutation(ctx, handle, actor, tokenID, runtimeID, action, payload, mutate)
			},
			Observe: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
				observation.writeObservation(ctx, handle, actor, tokenID, runtimeID, action, payload)
			},
			RepairProjection: func(ctx context.Context, id int64) error {
				if err := observation.syncVaultActionRequest(ctx, handle, id); err != nil {
					log.Printf("Vault request history projection repair failed request=%d error=%v", id, err)
					return err
				}
				return nil
			},
			RedactRequestError: func(ctx context.Context, err error) string {
				if err == nil {
					return ""
				}
				return capability.Policy.Redact(ctx, err.Error())
			},
			RedactRequestValue: func(ctx context.Context, value any) (any, error) {
				return gatewayvault.RedactRequestProjection(ctx, value, capability.Policy.Redact)
			},
			SealRequest: func(id int64, value any) (string, error) {
				return capability.SealVaultActionRequest(identity.WorkspaceID, id, value)
			},
			OpenRequest: func(id int64, sealed string, target any) error {
				return capability.OpenVaultActionRequest(identity.WorkspaceID, id, sealed, target)
			},
		},
	}, true
}

// VaultRuntime is the low-level composition projection used by boundary tests.
// Production API code must consume the behavioral application methods below.
func (component *VaultOwner) VaultRuntime(handle *WorkspaceHandle, ports VaultRuntimePorts) (gatewayvault.Runtime, bool) {
	return component.vaultRuntime(handle, ports)
}

func (component *VaultOwner) VaultActionApplication(handle *WorkspaceHandle, application *gatewayvault.Component, ports VaultRuntimePorts) (gatewayvault.VaultActionApplication, error) {
	runtime, ok := component.vaultRuntime(handle, ports)
	if !ok || application == nil {
		return nil, ErrWorkspaceHandleUnavailable
	}
	return application.ActionRuntime(runtime)
}

func (component *VaultOwner) VaultRequestApplication(ctx context.Context, handle *WorkspaceHandle, application *gatewayvault.Component, ports VaultRuntimePorts) (gatewayvault.VaultRequestApplication, error) {
	runtime, ok := component.vaultRuntime(handle, ports)
	if !ok || application == nil {
		return nil, ErrWorkspaceHandleUnavailable
	}
	return application.RequestRuntime(ctx, runtime)
}

func (component *VaultOwner) ReleaseVaultWorkspace(handle *WorkspaceHandle, application *gatewayvault.Component, ports VaultRuntimePorts) bool {
	runtime, ok := component.vaultRuntime(handle, ports)
	if !ok || application == nil {
		return false
	}
	application.ReleaseWorkspace(runtime)
	return true
}

type VaultSessionPorts struct {
	Principal func() (gatewayaccess.Principal, error)
	Requests  func(context.Context) (gatewayvault.RequestInvalidator, error)
}

func (component *VaultOwner) vaultSessionRuntime(handle *WorkspaceHandle, ports VaultSessionPorts) (gatewayvault.SessionLifecycleRuntime, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return gatewayvault.SessionLifecycleRuntime{}, false
	}
	projected := capabilities.Session
	capability, ok := projected.Current()
	if !ok || capability.Delivery == nil {
		return gatewayvault.SessionLifecycleRuntime{}, false
	}
	return gatewayvault.SessionLifecycleRuntime{
		Database: capability.Database, Leases: capability.Leases, Sessions: capability.Sessions,
		Principal: ports.Principal, Requests: ports.Requests, AcquireDelivery: capability.Delivery.AcquireDelivery,
		InstallAuthorizer: func(guard gatewayvault.SessionAuthorizationGuard) {
			capability.InstallAuthorizer(guard)
		},
		InstallSessionClosed: func(hook func(context.Context, gatewayvault.VaultSessionReference) error) {
			capability.InstallSessionClosed(func(ctx context.Context, sessionID, runtimeID, generation int64) error {
				return hook(ctx, gatewayvault.VaultSessionReference{
					SessionID: sessionID, RuntimeID: runtimeID, Generation: generation,
				})
			})
		},
	}, true
}

func (component *VaultOwner) VaultSessionLifecycle(handle *WorkspaceHandle, application *gatewayvault.Component, runtimePorts VaultRuntimePorts, sessionPorts VaultSessionPorts) (*gatewayvault.SessionLifecycle, error) {
	if application == nil {
		return nil, gatewayvault.InvalidatorUnavailableError()
	}
	sessionPorts.Requests = func(ctx context.Context) (gatewayvault.RequestInvalidator, error) {
		return component.VaultRequestApplication(ctx, handle, application, runtimePorts)
	}
	runtime, ok := component.vaultSessionRuntime(handle, sessionPorts)
	if !ok {
		return nil, gatewayvault.InvalidatorUnavailableError()
	}
	return application.SessionLifecycle(runtime)
}

func (component *VaultOwner) StopMCP(ctx context.Context, handle *WorkspaceHandle, application *gatewayvault.Component, runtimePorts VaultRuntimePorts, sessionPorts VaultSessionPorts) error {
	lifecycle, err := component.VaultSessionLifecycle(handle, application, runtimePorts, sessionPorts)
	if err != nil {
		return err
	}
	requests, err := component.VaultRequestApplication(ctx, handle, application, runtimePorts)
	if err != nil {
		return err
	}
	return application.StopMCP(ctx, lifecycle, requests)
}

type VaultMCPPorts struct {
	TokenID      int64
	Runtime      func(context.Context) (gatewayvault.VaultRequestApplication, error)
	MetadataRead func(context.Context, int64) (bool, error)
}

func (component *VaultOwner) vaultMCPScope(handle *WorkspaceHandle, ports VaultMCPPorts) (gatewayvault.VaultMCPHTTPScope, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return gatewayvault.VaultMCPHTTPScope{}, false
	}
	projected := capabilities.MCP
	capability, ok := projected.Current()
	if !ok || capability.Control == nil {
		return gatewayvault.VaultMCPHTTPScope{}, false
	}
	return gatewayvault.VaultMCPHTTPScope{
		Database: capability.Database, Vault: capability.Vault,
		WorkspaceUUID: handle.Identity().WorkspaceID, TokenID: ports.TokenID,
		MCPStarted: capability.Control.MCPStarted,
		Runtime:    ports.Runtime, MetadataRead: ports.MetadataRead,
	}, true
}

func (component *VaultOwner) vaultApprovalScope(handle *WorkspaceHandle, runtime func(context.Context) (gatewayvault.VaultRequestApplication, error)) (gatewayvault.VaultApprovalHTTPScope, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return gatewayvault.VaultApprovalHTTPScope{}, false
	}
	projected := capabilities.Approval
	capability, ok := projected.Current()
	if !ok || capability.Control == nil {
		return gatewayvault.VaultApprovalHTTPScope{}, false
	}
	return gatewayvault.VaultApprovalHTTPScope{
		MCPStarted: capability.Control.MCPStarted, Runtime: runtime,
	}, true
}
