package gatewayinfrastructure

import (
	"sync"

	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

type ownerBase struct{ owner *Component }

func (boundary ownerBase) valid(handle *WorkspaceHandle) bool {
	return boundary.owner != nil && boundary.owner.owns(handle)
}

func (boundary ownerBase) belongs(handle *WorkspaceHandle) bool {
	return boundary.owner != nil && handle != nil && handle.component == boundary.owner.identity
}

func (boundary ownerBase) handleFor(runtime *gatewayworkspace.Runtime) *WorkspaceHandle {
	if boundary.owner == nil {
		return nil
	}
	return boundary.owner.handleFor(runtime)
}

func (boundary ownerBase) forgetHandle(handle *WorkspaceHandle) {
	if boundary.owner != nil {
		boundary.owner.forgetHandle(handle)
	}
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
}
type ConnectorActionOwner struct {
	ownerBase
	capabilities capabilityRegistry[gatewayworkspace.ConnectorActions]
}
type ConnectorManagementOwner struct {
	ownerBase
	capabilities capabilityRegistry[gatewayworkspace.ConnectorManagement]
}
type ConnectorPortsOwner struct {
	ownerBase
	capabilities capabilityRegistry[gatewayworkspace.ConnectorPorts]
}
type ObservationOwner struct {
	ownerBase
	capabilities capabilityRegistry[gatewayworkspace.Observation]
}
type OperationsOwner struct {
	ownerBase
	capabilities capabilityRegistry[gatewayworkspace.Operations]
}
type VaultOwner struct {
	ownerBase
	capabilities capabilityRegistry[gatewayworkspace.Vault]
}
type WorkspaceOwner struct{ ownerBase }

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
	base := ownerBase{owner: component}
	component.accessOwner = &AccessOwner{ownerBase: base}
	component.actionOwner = &ConnectorActionOwner{ownerBase: base}
	component.managementOwner = &ConnectorManagementOwner{ownerBase: base}
	component.portsOwner = &ConnectorPortsOwner{ownerBase: base}
	component.observationOwner = &ObservationOwner{ownerBase: base}
	component.operationsOwner = &OperationsOwner{ownerBase: base}
	component.vaultOwner = &VaultOwner{ownerBase: base}
	component.workspaceOwner = &WorkspaceOwner{ownerBase: base}
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
