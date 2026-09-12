package gatewayinfrastructure

import (
	"context"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (component *AccessOwner) MCPTokenSource(handle *WorkspaceHandle) (gatewayaccess.MCPTokenSource, bool) {
	owner, ok := component.resolve(handle)
	if !ok || owner.Storage.TokenStore() == nil {
		return nil, false
	}
	return owner.Storage.TokenStore(), true
}

type MCPReadPorts struct {
	TokenID         int64
	Permissions     func(context.Context) ([]gatewayaccess.MCPPermission, error)
	MetadataEnabled func(context.Context) (bool, error)
	Metadata        gatewayaccess.MCPMetadataResolver
}

func (component *AccessOwner) MCPReadScope(handle *WorkspaceHandle, ports MCPReadPorts) (gatewayaccess.MCPScope, bool) {
	owner, ok := component.resolve(handle)
	if !ok || !ports.Metadata.Ready() {
		return gatewayaccess.MCPScope{}, false
	}
	return gatewayaccess.MCPScope{
		Database: owner.Storage.DatabaseHandle(), Registry: owner.Connectors.ConnectorRegistry(),
		TokenID: ports.TokenID, Permissions: ports.Permissions,
		MetadataEnabled: ports.MetadataEnabled, Metadata: ports.Metadata,
	}, true
}

type MCPActionPorts struct {
	TokenID     int64
	Delivery    func(func(context.Context) (func(), error)) gatewayactions.DeliveryGate
	Principal   func(int64) (gatewayaccess.Principal, error)
	Call        func(context.Context, gatewayaccess.MCPActionCall) (gatewayaccess.MCPActionCallResult, error)
	Observe     func(context.Context, string, any)
	Redact      func(context.Context, string) string
	RunningHint gatewayaccess.MCPRunningHint
}

func (component *AccessOwner) MCPActionScope(handle *WorkspaceHandle, ports MCPActionPorts) (gatewayaccess.MCPActionScope, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return gatewayaccess.MCPActionScope{}, false
	}
	if ports.Delivery == nil {
		return gatewayaccess.MCPActionScope{}, false
	}
	output := &gatewayaccess.MCPOutputAuthorization{
		Database: owner.Storage.DatabaseHandle(), Tokens: owner.Storage.TokenStore(),
		Leases: owner.Security.VaultLeaseStore(), Delivery: ports.Delivery(owner.Security.VaultDeliveryCoordinator().AcquireDelivery),
		MCPStarted: owner.Security.RuntimeControlState().MCPStarted, Principal: ports.Principal,
	}
	return gatewayaccess.MCPActionScope{
		Database: owner.Storage.DatabaseHandle(), TokenID: ports.TokenID, Output: output,
		Call: ports.Call, Observe: ports.Observe, Redact: ports.Redact, RunningHint: ports.RunningHint,
	}, true
}

type MCPRuntimePorts struct {
	StartEnabled func(context.Context) (bool, error)
	StopEffects  func(context.Context) error
	Observe      func(context.Context, string, map[string]any)
}

func (component *AccessOwner) MCPRuntimeScope(handle *WorkspaceHandle, ports MCPRuntimePorts) (gatewayaccess.MCPRuntimeScope, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return gatewayaccess.MCPRuntimeScope{}, false
	}
	return gatewayaccess.MCPRuntimeScope{
		State: owner.Security.RuntimeControlState(), StartEnabled: ports.StartEnabled,
		AcquireStop: owner.Security.VaultDeliveryCoordinator().AcquireExclusive,
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
	owner, ok := component.resolve(handle)
	if !ok || owner.Connectors.ConsoleSessionManager() == nil {
		return nil, ErrWorkspaceHandleUnavailable
	}
	return owner.Connectors.ConsoleSessionManager().RecoverRuntime(ctx, principal, runtimeID, cancelRunning)
}

func (component *AccessOwner) SecurityScope(handle *WorkspaceHandle) (gatewayaccess.SecurityHTTPScope, bool) {
	owner, ok := component.resolve(handle)
	if !ok || owner.Security.PolicyService() == nil {
		return gatewayaccess.SecurityHTTPScope{}, false
	}
	return gatewayaccess.SecurityHTTPScope{
		Service: owner.Security.PolicyService(),
		Mutate:  gatewayaccess.MutationRunner(component.owner.ObservationOwner().mutationRunner(handle, "user", nil, 0)),
	}, true
}

func (component *AccessOwner) ReadSecuritySettings(ctx context.Context, handle *WorkspaceHandle) (gatewayaccess.SecuritySettings, error) {
	owner, ok := component.resolve(handle)
	if !ok || owner.Security.PolicyService() == nil {
		return gatewayaccess.SecuritySettings{}, ErrWorkspaceHandleUnavailable
	}
	return owner.Security.PolicyService().ReadSettings(ctx)
}

func (component *AccessOwner) RedactForPersistence(ctx context.Context, handle *WorkspaceHandle, value string) (string, bool) {
	owner, ok := component.resolve(handle)
	if !ok || owner.Security.PolicyService() == nil {
		return "", false
	}
	return owner.Security.PolicyService().Redact(ctx, value), true
}

func (component *AccessOwner) RedactCustom(ctx context.Context, handle *WorkspaceHandle, value string) (string, bool) {
	owner, ok := component.resolve(handle)
	if !ok || owner.Security.PolicyService() == nil {
		return "", false
	}
	return owner.Security.PolicyService().RedactCustom(ctx, value), true
}

func (component *AccessOwner) RuntimeRedactor(handle *WorkspaceHandle) (func(string) string, bool) {
	owner, ok := component.resolve(handle)
	if !ok || owner.Security.PolicyService() == nil {
		return nil, false
	}
	return owner.Security.PolicyService().Redactor(), true
}

func (component *AccessOwner) ConfigureWorkspaceRuntime(
	ctx context.Context,
	handle *WorkspaceHandle,
	opener gatewayoperations.RuntimeOpener,
) error {
	owner, ok := component.resolve(handle)
	if !ok || owner.Security.PolicyService() == nil {
		return ErrWorkspaceHandleUnavailable
	}
	settings, err := owner.Security.PolicyService().ReadSettings(ctx)
	if err != nil {
		return err
	}
	owner.Security.RuntimeControlState().SetMCPStarted(settings.MCPStartEnabled)
	owner.Connectors.ConfigureConsoleSessions(gatewayoperations.AdaptRuntimeOpener(opener), owner.Security.PolicyService().Redactor())
	return nil
}

func (component *AccessOwner) ConfigureConsoleRuntime(
	handle *WorkspaceHandle,
	opener gatewayoperations.RuntimeOpener,
	redact func(string) string,
) error {
	owner, ok := component.resolve(handle)
	if !ok {
		return ErrWorkspaceHandleUnavailable
	}
	owner.Connectors.ConfigureConsoleSessions(gatewayoperations.AdaptRuntimeOpener(opener), redact)
	return nil
}

func (component *OperationsOwner) MessageStore(handle *WorkspaceHandle, redact func(context.Context, string) string) (*gatewayoperations.MessageStore, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return nil, false
	}
	return gatewayoperations.NewMessageStore(owner.Storage.DatabaseHandle(), redact), true
}

type ProjectPorts struct {
	Invalidate func(context.Context, int64) error
}

func (component *VaultOwner) ProjectScope(handle *WorkspaceHandle, ports ProjectPorts) (gatewayvault.ProjectScope, bool) {
	owner, ok := component.resolve(handle)
	if !ok {
		return gatewayvault.ProjectScope{}, false
	}
	return gatewayvault.ProjectScope{
		Database:         owner.Storage.DatabaseHandle(),
		Mutate:           gatewayvault.ProjectMutation(component.owner.ObservationOwner().mutationRunner(handle, "user", nil, 0)),
		AcquireExclusive: owner.Security.VaultDeliveryCoordinator().AcquireExclusive,
		Invalidate:       ports.Invalidate,
	}, true
}
