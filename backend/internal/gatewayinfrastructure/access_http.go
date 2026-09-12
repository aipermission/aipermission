package gatewayinfrastructure

import (
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

type AccessHTTPDependencies struct {
	Active      func(http.ResponseWriter) (*WorkspaceHandle, bool)
	TokenAccess func(*WorkspaceHandle) AccessControlPorts
	MCPRuntime  func(*WorkspaceHandle) MCPRuntimePorts
	MCPRead     func(http.ResponseWriter, *http.Request) (*WorkspaceHandle, MCPReadPorts, bool)
	MCPAction   func(http.ResponseWriter, *http.Request) (*WorkspaceHandle, MCPActionPorts, bool)
}

func (component *AccessOwner) HTTPHandlers(
	application *gatewayaccess.Component,
	factory gatewayaccess.HTTPHandlerFactory,
	dependencies AccessHTTPDependencies,
) gatewayaccess.HTTPHandlers {
	if component == nil || application == nil || factory == nil || dependencies.Active == nil ||
		dependencies.TokenAccess == nil || dependencies.MCPRuntime == nil ||
		dependencies.MCPRead == nil || dependencies.MCPAction == nil {
		return gatewayaccess.HTTPHandlers{}
	}
	return factory.Build(gatewayaccess.ScopeProviders{
		Security: func(w http.ResponseWriter) (gatewayaccess.SecurityHTTPScope, bool) {
			handle, ok := dependencies.Active(w)
			if !ok {
				return gatewayaccess.SecurityHTTPScope{}, false
			}
			return component.securityScope(handle)
		},
		TokenAccess: func(w http.ResponseWriter) (gatewayaccess.AccessScope, bool) {
			handle, ok := dependencies.Active(w)
			if !ok {
				return gatewayaccess.AccessScope{}, false
			}
			return component.accessControlWorkspace(handle, dependencies.TokenAccess(handle))
		},
		MCPRuntime: func(w http.ResponseWriter) (gatewayaccess.MCPRuntimeScope, bool) {
			handle, ok := dependencies.Active(w)
			if !ok {
				return gatewayaccess.MCPRuntimeScope{}, false
			}
			return component.mcpRuntimeScope(handle, dependencies.MCPRuntime(handle))
		},
		MCPConnectorReads: func(w http.ResponseWriter, r *http.Request) (gatewayaccess.MCPScope, bool) {
			handle, ports, ok := dependencies.MCPRead(w, r)
			if !ok {
				return gatewayaccess.MCPScope{}, false
			}
			return component.mcpReadScope(handle, ports)
		},
		MCPConnectorActions: func(w http.ResponseWriter, r *http.Request) (gatewayaccess.MCPActionScope, bool) {
			handle, ports, ok := dependencies.MCPAction(w, r)
			if !ok {
				return gatewayaccess.MCPActionScope{}, false
			}
			return component.mcpActionScope(handle, ports)
		},
	})
}
