package gatewayinfrastructure

import (
	"context"
	"database/sql"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	gatewaymanagement "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (component *AccessOwner) MCPTokenSource(handle *WorkspaceHandle) (gatewayaccess.MCPTokenSource, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return nil, false
	}
	projected := capabilities.MCPTokenSource
	return projected.Current()
}

type MCPReadPorts struct {
	TokenID         int64
	Permissions     func(context.Context) ([]gatewayaccess.MCPPermission, error)
	MetadataEnabled func(context.Context) (bool, error)
	Metadata        gatewayaccess.MCPMetadataResolver
}

func (component *AccessOwner) mcpReadScope(handle *WorkspaceHandle, ports MCPReadPorts) (gatewayaccess.MCPScope, bool) {
	capabilities, available := component.projection(handle)
	if !available || !ports.Metadata.Ready() {
		return gatewayaccess.MCPScope{}, false
	}
	projected := capabilities.MCPRead
	capability, ok := projected.Current()
	if !ok {
		return gatewayaccess.MCPScope{}, false
	}
	return gatewayaccess.MCPScope{
		Database: capability.Database, Registry: capability.Registry,
		TokenID: ports.TokenID, Permissions: ports.Permissions,
		MetadataEnabled: ports.MetadataEnabled, Metadata: ports.Metadata,
	}, true
}

type MCPActionPorts struct {
	TokenID     int64
	Delivery    func(func(context.Context) (func(), error)) gatewayactions.DeliveryGate
	Principal   func(int64) (gatewayaccess.Principal, error)
	Policy      gatewaymanagement.Catalog
	Call        func(context.Context, gatewayaccess.MCPActionCall) (gatewayaccess.MCPActionCallResult, error)
	Observe     func(context.Context, string, any)
	Redact      func(context.Context, string) string
	RunningHint gatewayaccess.MCPRunningHint
}

func (component *AccessOwner) mcpActionScope(handle *WorkspaceHandle, ports MCPActionPorts) (gatewayaccess.MCPActionScope, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return gatewayaccess.MCPActionScope{}, false
	}
	projected := capabilities.MCPAction
	capability, ok := projected.Current()
	if !ok || capability.Delivery == nil || capability.Control == nil {
		return gatewayaccess.MCPActionScope{}, false
	}
	if ports.Delivery == nil || !ports.Policy.Ready() ||
		ports.Call == nil || ports.Observe == nil || ports.Redact == nil || ports.Principal == nil {
		return gatewayaccess.MCPActionScope{}, false
	}
	output := &gatewayaccess.MCPOutputAuthorization{
		Database: capability.Database, Tokens: capability.Tokens,
		Leases: capability.Leases, Delivery: ports.Delivery(capability.Delivery.AcquireDelivery),
		MCPStarted: capability.Control.MCPStarted, Principal: ports.Principal,
	}
	return gatewayaccess.MCPActionScope{
		Database: capability.Database, RuntimeID: handle.Identity().RuntimeID, TokenID: ports.TokenID, Output: output,
		ActionVisible: func(ctx context.Context, targetRef, actionName string) (bool, error) {
			return ports.Policy.MCPActionVisible(ctx, ports.TokenID, targetRef, actionName)
		},
		ReplayExists: func(ctx context.Context, idempotencyKey string) (bool, error) {
			return ports.Policy.MCPReplayExists(ctx, ports.TokenID, idempotencyKey)
		},
		ResourcePolicy: func(ctx context.Context, targetRef, actionName string) (gatewayaccess.MCPActionResourcePolicy, error) {
			policy, err := ports.Policy.MCPActionResourcePolicy(ctx, ports.TokenID, targetRef, actionName)
			return gatewayaccess.MCPActionResourcePolicy{
				MaxInputBytes: policy.MaxInputBytes,
			}, err
		},
		Call: ports.Call, Observe: ports.Observe, Redact: ports.Redact, RunningHint: ports.RunningHint,
	}, true
}

type MCPRuntimePorts struct {
	StartEnabled func(context.Context) (bool, error)
	StopEffects  func(context.Context) error
	Observe      func(context.Context, string, map[string]any)
}

func (component *AccessOwner) mcpRuntimeScope(handle *WorkspaceHandle, ports MCPRuntimePorts) (gatewayaccess.MCPRuntimeScope, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return gatewayaccess.MCPRuntimeScope{}, false
	}
	projected := capabilities.MCPRuntime
	capability, ok := projected.Current()
	if !ok || capability.Control == nil || capability.Delivery == nil {
		return gatewayaccess.MCPRuntimeScope{}, false
	}
	return gatewayaccess.MCPRuntimeScope{
		State: capability.Control, StartEnabled: ports.StartEnabled,
		AcquireStop: capability.Delivery.AcquireExclusive,
		StopEffects: ports.StopEffects, Observe: ports.Observe,
	}, true
}

func (component *AccessOwner) RecoverConsoleRuntime(
	ctx context.Context,
	handle *WorkspaceHandle,
	principal gatewayaccess.Principal,
	runtimeID int64,
	cancelRunning func() error,
) ([]int64, error) {
	capabilities, available := component.projection(handle)
	if !available {
		return nil, ErrWorkspaceHandleUnavailable
	}
	projected := capabilities.ConsoleRecovery
	capability, ok := projected.Current()
	if !ok {
		return nil, ErrWorkspaceHandleUnavailable
	}
	return capability.Recover(ctx, principal, runtimeID, cancelRunning)
}

func (component *AccessOwner) securityScope(handle *WorkspaceHandle) (gatewayaccess.SecurityHTTPScope, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return gatewayaccess.SecurityHTTPScope{}, false
	}
	projected := capabilities.SecurityPolicy
	capability, ok := projected.Current()
	if !ok {
		return gatewayaccess.SecurityHTTPScope{}, false
	}
	return gatewayaccess.SecurityHTTPScope{
		Service: capability.Policy,
		Mutate:  gatewayaccess.MutationRunner(component.observation.observationMutationRunner(handle, "user", nil, 0)),
	}, true
}

func (component *AccessOwner) ReadSecuritySettings(ctx context.Context, handle *WorkspaceHandle) (gatewayaccess.SecuritySettings, error) {
	capabilities, available := component.projection(handle)
	if !available {
		return gatewayaccess.SecuritySettings{}, ErrWorkspaceHandleUnavailable
	}
	projected := capabilities.SecurityPolicy
	capability, ok := projected.Current()
	if !ok {
		return gatewayaccess.SecuritySettings{}, ErrWorkspaceHandleUnavailable
	}
	return capability.Policy.ReadSettings(ctx)
}

func (component *AccessOwner) UpdateSecuritySettings(
	ctx context.Context,
	handle *WorkspaceHandle,
	settings gatewayaccess.SecuritySettings,
) (gatewayaccess.SecuritySettings, error) {
	capabilities, available := component.projection(handle)
	if !available {
		return gatewayaccess.SecuritySettings{}, ErrWorkspaceHandleUnavailable
	}
	projected := capabilities.SecurityPolicy
	capability, ok := projected.Current()
	if !ok {
		return gatewayaccess.SecuritySettings{}, ErrWorkspaceHandleUnavailable
	}
	mutate := component.observation.observationMutationRunner(handle, "user", nil, 0)
	if mutate == nil {
		return gatewayaccess.SecuritySettings{}, ErrWorkspaceHandleUnavailable
	}
	return capability.Policy.UpdateSettings(
		ctx,
		settings,
		func(ctx context.Context, action string, payload func() any, change func(*sql.Tx) error) error {
			return mutate(ctx, action, payload, change)
		},
	)
}

func (component *AccessOwner) CreateSecurityRule(
	ctx context.Context,
	handle *WorkspaceHandle,
	input gatewayaccess.SecurityRuleInput,
) (gatewayaccess.SecurityRule, error) {
	capabilities, available := component.projection(handle)
	if !available {
		return gatewayaccess.SecurityRule{}, ErrWorkspaceHandleUnavailable
	}
	projected := capabilities.SecurityPolicy
	capability, ok := projected.Current()
	if !ok {
		return gatewayaccess.SecurityRule{}, ErrWorkspaceHandleUnavailable
	}
	mutate := component.observation.observationMutationRunner(handle, "user", nil, 0)
	if mutate == nil {
		return gatewayaccess.SecurityRule{}, ErrWorkspaceHandleUnavailable
	}
	return capability.Policy.CreateRule(
		ctx,
		input,
		func(ctx context.Context, action string, payload func() any, change func(*sql.Tx) error) error {
			return mutate(ctx, action, payload, change)
		},
	)
}

func (component *AccessOwner) MCPStarted(handle *WorkspaceHandle) (bool, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return false, false
	}
	projected := capabilities.RuntimeControl
	capability, ok := projected.Current()
	if !ok || capability.MCPStarted == nil {
		return false, false
	}
	return capability.MCPStarted(), true
}

func (component *AccessOwner) SetMCPStarted(handle *WorkspaceHandle, enabled bool) bool {
	capabilities, available := component.projection(handle)
	if !available {
		return false
	}
	projected := capabilities.RuntimeControl
	capability, ok := projected.Current()
	if !ok || capability.SetMCPStarted == nil {
		return false
	}
	capability.SetMCPStarted(enabled)
	return true
}

func (component *AccessOwner) RedactForPersistence(ctx context.Context, handle *WorkspaceHandle, value string) (string, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return "", false
	}
	projected := capabilities.SecurityPolicy
	capability, ok := projected.Current()
	if !ok {
		return "", false
	}
	return capability.Policy.Redact(ctx, value), true
}

func (component *AccessOwner) RedactCustom(ctx context.Context, handle *WorkspaceHandle, value string) (string, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return "", false
	}
	projected := capabilities.SecurityPolicy
	capability, ok := projected.Current()
	if !ok {
		return "", false
	}
	return capability.Policy.RedactCustom(ctx, value), true
}

func (component *AccessOwner) RuntimeRedactor(handle *WorkspaceHandle) (func(string) string, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return nil, false
	}
	projected := capabilities.SecurityPolicy
	capability, ok := projected.Current()
	if !ok {
		return nil, false
	}
	return capability.Policy.Redactor(), true
}

func (component *AccessOwner) ConfigureWorkspaceRuntime(
	ctx context.Context,
	handle *WorkspaceHandle,
	opener gatewayoperations.RuntimeOpener,
) error {
	capabilities, available := component.projection(handle)
	if !available {
		return ErrWorkspaceHandleUnavailable
	}
	projected := capabilities.RuntimeConfiguration
	capability, ok := projected.Current()
	if !ok || capability.Control == nil || capability.ConfigureConsole == nil {
		return ErrWorkspaceHandleUnavailable
	}
	settings, err := capability.Policy.ReadSettings(ctx)
	if err != nil {
		return err
	}
	capability.Control.SetMCPStarted(settings.MCPStartEnabled)
	capability.ConfigureConsole(gatewayoperations.AdaptRuntimeOpener(opener), capability.Policy.Redactor())
	return nil
}

func (component *AccessOwner) ConfigureConsoleRuntime(
	handle *WorkspaceHandle,
	opener gatewayoperations.RuntimeOpener,
	redact func(string) string,
) error {
	capabilities, available := component.projection(handle)
	if !available {
		return ErrWorkspaceHandleUnavailable
	}
	projected := capabilities.ConsoleConfiguration
	capability, ok := projected.Current()
	if !ok || capability.Configure == nil {
		return ErrWorkspaceHandleUnavailable
	}
	capability.Configure(gatewayoperations.AdaptRuntimeOpener(opener), redact)
	return nil
}

func (component *OperationsOwner) MessageStore(handle *WorkspaceHandle, redact func(context.Context, string) string) (*gatewayoperations.MessageStore, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return nil, false
	}
	projected := capabilities.Message
	capability, ok := projected.Current()
	if !ok {
		return nil, false
	}
	return gatewayoperations.NewMessageStore(capability.Database, redact), true
}

type ProjectPorts struct {
	Invalidate func(context.Context, int64) error
}

func (component *VaultOwner) projectScope(handle *WorkspaceHandle, ports ProjectPorts) (gatewayvault.ProjectScope, bool) {
	capabilities, available := component.projection(handle)
	if !available {
		return gatewayvault.ProjectScope{}, false
	}
	projected := capabilities.Project
	capability, ok := projected.Current()
	if !ok || capability.Delivery == nil {
		return gatewayvault.ProjectScope{}, false
	}
	return gatewayvault.ProjectScope{
		Database:         capability.Database,
		Mutate:           gatewayvault.ProjectMutation(component.observation.observationMutationRunner(handle, "user", nil, 0)),
		AcquireExclusive: capability.Delivery.AcquireExclusive,
		Invalidate:       ports.Invalidate,
	}, true
}
