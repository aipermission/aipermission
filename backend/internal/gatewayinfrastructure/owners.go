package gatewayinfrastructure

import "github.com/aipermission/aipermission/backend/internal/gatewayworkspace"

type ownerBase struct{ owner *Component }

func (boundary ownerBase) valid(handle *WorkspaceHandle) bool {
	return boundary.owner != nil && boundary.owner.owns(handle)
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

type AccessOwner struct{ ownerBase }
type ConnectorActionOwner struct{ ownerBase }
type ConnectorManagementOwner struct{ ownerBase }
type ConnectorPortsOwner struct{ ownerBase }
type ObservationOwner struct{ ownerBase }
type OperationsOwner struct{ ownerBase }
type VaultOwner struct{ ownerBase }
type WorkspaceOwner struct{ ownerBase }

func (component *AccessOwner) resolve(handle *WorkspaceHandle) (*gatewayworkspace.AccessCapabilities, bool) {
	if component == nil || !component.valid(handle) {
		return nil, false
	}
	return &handle.access, true
}

func (component *ConnectorActionOwner) resolve(handle *WorkspaceHandle) (*gatewayworkspace.ConnectorActionCapabilities, bool) {
	if component == nil || !component.valid(handle) {
		return nil, false
	}
	return &handle.connectorActions, true
}

func (component *ConnectorManagementOwner) resolve(handle *WorkspaceHandle) (*gatewayworkspace.ConnectorManagementCapabilities, bool) {
	if component == nil || !component.valid(handle) {
		return nil, false
	}
	return &handle.connectorManagement, true
}

func (component *ConnectorPortsOwner) resolve(handle *WorkspaceHandle) (*gatewayworkspace.ConnectorPortsCapabilities, bool) {
	if component == nil || !component.valid(handle) {
		return nil, false
	}
	return &handle.connectorPorts, true
}

func (component *ObservationOwner) resolve(handle *WorkspaceHandle) (*gatewayworkspace.ObservationCapabilities, bool) {
	if component == nil || !component.valid(handle) {
		return nil, false
	}
	return &handle.observation, true
}

func (component *OperationsOwner) resolve(handle *WorkspaceHandle) (*gatewayworkspace.OperationsCapabilities, bool) {
	if component == nil || !component.valid(handle) {
		return nil, false
	}
	return &handle.operations, true
}

func (component *VaultOwner) resolve(handle *WorkspaceHandle) (*gatewayworkspace.VaultCapabilities, bool) {
	if component == nil || !component.valid(handle) {
		return nil, false
	}
	return &handle.vault, true
}

func (component *WorkspaceOwner) resolve(handle *WorkspaceHandle) (*gatewayworkspace.Runtime, bool) {
	if component == nil || !component.valid(handle) || handle.workspace == nil {
		return nil, false
	}
	return handle.workspace, true
}

func (component *Component) AccessOwner() *AccessOwner {
	return &AccessOwner{ownerBase{owner: component}}
}

func (component *Component) ConnectorActionOwner() *ConnectorActionOwner {
	return &ConnectorActionOwner{ownerBase{owner: component}}
}

func (component *Component) ConnectorManagementOwner() *ConnectorManagementOwner {
	return &ConnectorManagementOwner{ownerBase{owner: component}}
}

func (component *Component) ConnectorPortsOwner() *ConnectorPortsOwner {
	return &ConnectorPortsOwner{ownerBase{owner: component}}
}

func (component *Component) ObservationOwner() *ObservationOwner {
	return &ObservationOwner{ownerBase{owner: component}}
}

func (component *Component) OperationsOwner() *OperationsOwner {
	return &OperationsOwner{ownerBase{owner: component}}
}

func (component *Component) VaultOwner() *VaultOwner {
	return &VaultOwner{ownerBase{owner: component}}
}

func (component *Component) WorkspaceOwner() *WorkspaceOwner {
	return &WorkspaceOwner{ownerBase{owner: component}}
}
