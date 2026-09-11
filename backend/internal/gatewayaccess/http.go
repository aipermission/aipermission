package gatewayaccess

import (
	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

// HTTPScopeProviders collects the transport-facing access scopes at the
// gateway boundary. The component converts this composition input into the
// independently owned authorization handlers used by the route registry.
type HTTPScopeProviders struct {
	Security            securitypolicy.HTTPScopeProvider
	TokenAccess         accesscontrol.ScopeProvider
	BulkConsole         commandrequests.BulkHTTPScopeProvider
	CommandRequests     commandrequests.HTTPScopeProvider
	MCPRuntime          runtimecontrol.MCPRuntimeScopeProvider
	MCPConnectorReads   mcpconnector.ScopeProvider
	MCPConnectorActions mcpconnector.ActionScopeProvider
}

type HTTPHandlers struct {
	Security            *securitypolicy.HTTPHandlers
	TokenAccess         *accesscontrol.HTTPHandlers
	BulkConsole         *commandrequests.BulkHTTPHandlers
	CommandRequests     *commandrequests.HTTPHandlers
	MCPRuntime          *runtimecontrol.MCPHTTPHandlers
	MCPConnectorReads   *mcpconnector.HTTPHandlers
	MCPConnectorActions *mcpconnector.ActionHTTPHandlers
}

func (component *Component) HTTPHandlers(providers HTTPScopeProviders) HTTPHandlers {
	if component == nil {
		return HTTPHandlers{}
	}
	return HTTPHandlers{
		Security:            securitypolicy.NewHTTPHandlers(providers.Security),
		TokenAccess:         accesscontrol.NewHTTPHandlers(providers.TokenAccess),
		BulkConsole:         commandrequests.NewBulkHTTPHandlers(providers.BulkConsole),
		CommandRequests:     commandrequests.NewHTTPHandlers(providers.CommandRequests),
		MCPRuntime:          runtimecontrol.NewMCPHTTPHandlers(providers.MCPRuntime),
		MCPConnectorReads:   mcpconnector.NewHTTPHandlers(providers.MCPConnectorReads),
		MCPConnectorActions: mcpconnector.NewActionHTTPHandlers(providers.MCPConnectorActions),
	}
}
