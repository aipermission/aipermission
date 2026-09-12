package gatewayinfrastructure

import "github.com/aipermission/aipermission/backend/internal/gatewayworkspace"

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

func (boundary ownerBase) resolve(handle *WorkspaceHandle) (*gatewayworkspace.Runtime, bool) {
	if !boundary.valid(handle) || handle.workspace == nil {
		return nil, false
	}
	return handle.workspace, true
}

type AccessOwner struct{ ownerBase }
type ConnectorActionOwner struct{ ownerBase }
type ConnectorManagementOwner struct{ ownerBase }
type ConnectorPortsOwner struct{ ownerBase }
type ObservationOwner struct{ ownerBase }
type OperationsOwner struct{ ownerBase }
type VaultOwner struct{ ownerBase }
type WorkspaceOwner struct{ ownerBase }

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
