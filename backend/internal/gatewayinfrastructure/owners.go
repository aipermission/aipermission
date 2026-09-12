package gatewayinfrastructure

import (
	"context"
	"sync"

	gatewaybackup "github.com/aipermission/aipermission/backend/internal/gatewayoperations/backup"
	observationapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/observation"
	gatewaytransfer "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

type ownerBase struct{ owner *componentIdentity }

func (boundary ownerBase) valid(handle *WorkspaceHandle) bool {
	return boundary.owner != nil && handle != nil && handle.component == boundary.owner && handle.active.Load()
}

func (boundary ownerBase) belongs(handle *WorkspaceHandle) bool {
	return boundary.owner != nil && handle != nil && handle.component == boundary.owner
}

type capabilityRegistry[T any] struct{ values sync.Map }

func (registry *capabilityRegistry[T]) bind(handle *WorkspaceHandle, capabilities T) {
	if registry != nil && handle != nil {
		registry.values.Store(handle, capabilities)
	}
}

func (registry *capabilityRegistry[T]) get(boundary ownerBase, handle *WorkspaceHandle) (T, bool) {
	var zero T
	if registry == nil || !boundary.valid(handle) {
		return zero, false
	}
	value, ok := registry.values.Load(handle)
	if !ok {
		return zero, false
	}
	capabilities, ok := value.(T)
	return capabilities, ok
}

func (registry *capabilityRegistry[T]) forget(handle *WorkspaceHandle) {
	if registry != nil && handle != nil {
		registry.values.Delete(handle)
	}
}

type AccessOwner struct {
	ownerBase
	capabilities capabilityRegistry[gatewayworkspace.Access]
	observation  *ObservationOwner
}
type ConnectorActionOwner struct {
	ownerBase
	capabilities capabilityRegistry[gatewayworkspace.ConnectorActions]
	observation  *ObservationOwner
}
type ConnectorManagementOwner struct {
	ownerBase
	capabilities capabilityRegistry[gatewayworkspace.ConnectorManagement]
	observation  *ObservationOwner
}
type ConnectorPortsOwner struct {
	ownerBase
	capabilities capabilityRegistry[gatewayworkspace.ConnectorPorts]
}
type ObservationOwner struct {
	ownerBase
	capabilities capabilityRegistry[gatewayworkspace.Observation]
	application  *observationapp.Component
}
type OperationsOwner struct {
	ownerBase
	capabilities           capabilityRegistry[gatewayworkspace.Operations]
	observation            *ObservationOwner
	backupLifecycle        gatewaybackup.Lifecycle
	acquireBackupOperation func(context.Context) (func(), error)
	transfers              *gatewaytransfer.Component
	transferHTTP           *gatewaytransfer.FileTransferHTTPHandlers
}
type VaultOwner struct {
	ownerBase
	capabilities capabilityRegistry[gatewayworkspace.Vault]
	observation  *ObservationOwner
}
type WorkspaceOwner struct {
	ownerBase
	workspace           *gatewayworkspace.Component
	handleForRuntime    func(*gatewayworkspace.Runtime) *WorkspaceHandle
	forgetRuntimeHandle func(*WorkspaceHandle)
	ownedHandles        func() []*WorkspaceHandle
	validatePassword    func(context.Context, *gatewayworkspace.Runtime, string, string) error
}

func (component *WorkspaceOwner) handleFor(runtime *gatewayworkspace.Runtime) *WorkspaceHandle {
	if component == nil || component.handleForRuntime == nil {
		return nil
	}
	return component.handleForRuntime(runtime)
}

func (component *WorkspaceOwner) forgetHandle(handle *WorkspaceHandle) {
	if component != nil && component.forgetRuntimeHandle != nil {
		component.forgetRuntimeHandle(handle)
	}
}

func (component *AccessOwner) projection(handle *WorkspaceHandle) (gatewayworkspace.Access, bool) {
	return component.capabilities.get(component.ownerBase, handle)
}
func (component *ConnectorActionOwner) projection(handle *WorkspaceHandle) (gatewayworkspace.ConnectorActions, bool) {
	return component.capabilities.get(component.ownerBase, handle)
}
func (component *ConnectorManagementOwner) projection(handle *WorkspaceHandle) (gatewayworkspace.ConnectorManagement, bool) {
	return component.capabilities.get(component.ownerBase, handle)
}
func (component *ConnectorPortsOwner) projection(handle *WorkspaceHandle) (gatewayworkspace.ConnectorPorts, bool) {
	return component.capabilities.get(component.ownerBase, handle)
}
func (component *ObservationOwner) projection(handle *WorkspaceHandle) (gatewayworkspace.Observation, bool) {
	return component.capabilities.get(component.ownerBase, handle)
}
func (component *OperationsOwner) projection(handle *WorkspaceHandle) (gatewayworkspace.Operations, bool) {
	return component.capabilities.get(component.ownerBase, handle)
}
func (component *VaultOwner) projection(handle *WorkspaceHandle) (gatewayworkspace.Vault, bool) {
	return component.capabilities.get(component.ownerBase, handle)
}

func (component *WorkspaceOwner) lifecycleRuntime(handle *WorkspaceHandle) (*gatewayworkspace.Runtime, bool) {
	if component == nil || !component.valid(handle) || handle.workspace == nil {
		return nil, false
	}
	return handle.workspace, true
}

func (component *Component) bindOwners() {
	base := ownerBase{owner: component.identity}
	observation := &ObservationOwner{ownerBase: base, application: component.observation}
	component.observationOwner = observation
	component.accessOwner = &AccessOwner{ownerBase: base, observation: observation}
	component.actionOwner = &ConnectorActionOwner{ownerBase: base, observation: observation}
	component.managementOwner = &ConnectorManagementOwner{ownerBase: base, observation: observation}
	component.portsOwner = &ConnectorPortsOwner{ownerBase: base}
	component.operationsOwner = &OperationsOwner{
		ownerBase: base, observation: observation, acquireBackupOperation: component.acquireBackupOperation,
		transfers: gatewaytransfer.NewComponent(),
	}
	component.vaultOwner = &VaultOwner{ownerBase: base, observation: observation}
}

func (component *Component) bindWorkspaceOwner() {
	base := ownerBase{owner: component.identity}
	operations := component.operationsOwner
	handleFor := component.handleFor
	component.workspaceOwner = &WorkspaceOwner{
		ownerBase: base, workspace: component.workspace,
		handleForRuntime: handleFor, forgetRuntimeHandle: component.forgetHandle,
		ownedHandles: component.ownedHandlesSnapshot,
		validatePassword: func(ctx context.Context, runtime *gatewayworkspace.Runtime, databaseName, password string) error {
			handle := handleFor(runtime)
			database, ok := operations.passwordValidationDatabase(handle)
			if !ok {
				return InitializationError()
			}
			return gatewaybackup.ValidateNewPassword(ctx, database, databaseName, password)
		},
	}
}

func (component *Component) bindHandleCapabilities(handle *WorkspaceHandle, projection gatewayworkspace.Projection) {
	component.accessOwner.capabilities.bind(handle, projection.Access)
	component.actionOwner.capabilities.bind(handle, projection.ConnectorActions)
	component.managementOwner.capabilities.bind(handle, projection.ConnectorManagement)
	component.portsOwner.capabilities.bind(handle, projection.ConnectorPorts)
	component.observationOwner.capabilities.bind(handle, projection.Observation)
	component.operationsOwner.capabilities.bind(handle, projection.Operations)
	component.vaultOwner.capabilities.bind(handle, projection.Vault)
}

func (component *Component) forgetHandleCapabilities(handle *WorkspaceHandle) {
	component.accessOwner.capabilities.forget(handle)
	component.actionOwner.capabilities.forget(handle)
	component.managementOwner.capabilities.forget(handle)
	component.portsOwner.capabilities.forget(handle)
	component.observationOwner.capabilities.forget(handle)
	component.operationsOwner.capabilities.forget(handle)
	component.vaultOwner.capabilities.forget(handle)
}

func (component *Component) AccessOwner() *AccessOwner {
	if component == nil {
		return nil
	}
	return component.accessOwner
}

func (component *Component) ConnectorActionOwner() *ConnectorActionOwner {
	if component == nil {
		return nil
	}
	return component.actionOwner
}

func (component *Component) ConnectorManagementOwner() *ConnectorManagementOwner {
	if component == nil {
		return nil
	}
	return component.managementOwner
}

func (component *Component) ConnectorPortsOwner() *ConnectorPortsOwner {
	if component == nil {
		return nil
	}
	return component.portsOwner
}

func (component *Component) ObservationOwner() *ObservationOwner {
	if component == nil {
		return nil
	}
	return component.observationOwner
}

func (component *Component) OperationsOwner() *OperationsOwner {
	if component == nil {
		return nil
	}
	return component.operationsOwner
}

func (component *Component) VaultOwner() *VaultOwner {
	if component == nil {
		return nil
	}
	return component.vaultOwner
}

func (component *Component) WorkspaceOwner() *WorkspaceOwner {
	if component == nil {
		return nil
	}
	return component.workspaceOwner
}
