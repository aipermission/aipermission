package gatewayaccess

import (
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/uisession"
)

func (*Component) NewCapabilityStore(db *sql.DB) *accesscontrol.CapabilityStore {
	return accesscontrol.NewCapabilityStore(db)
}

func (*Component) NewAccessHTTPHandlers(scope accesscontrol.ScopeProvider) *accesscontrol.HTTPHandlers {
	return accesscontrol.NewHTTPHandlers(scope)
}

func (*Component) NewBulkHTTPHandlers(scope commandrequests.BulkHTTPScopeProvider) *commandrequests.BulkHTTPHandlers {
	return commandrequests.NewBulkHTTPHandlers(scope)
}

func (*Component) NewCommandHTTPHandlers(scope commandrequests.HTTPScopeProvider) *commandrequests.HTTPHandlers {
	return commandrequests.NewHTTPHandlers(scope)
}

func (*Component) NewCommandWorkspaceRuntime(dependencies commandrequests.WorkspaceRuntimeDependencies) (*commandrequests.Runtime, error) {
	return commandrequests.NewWorkspaceRuntime(dependencies)
}

func (*Component) NewRuntimeInstanceID() (string, error) {
	return executionprincipal.NewRuntimeInstanceID()
}

func (*Component) PrincipalLocalOperator(workspaceID string, runtimeInstanceID string) (executionprincipal.Principal, error) {
	return executionprincipal.LocalOperator(workspaceID, runtimeInstanceID)
}

func (*Component) PrincipalMCPToken(tokenID int64, workspaceID string, runtimeInstanceID string) (executionprincipal.Principal, error) {
	return executionprincipal.MCPToken(tokenID, workspaceID, runtimeInstanceID)
}

func (*Component) NewMCPActionHTTPHandlers(scope mcpconnector.ActionScopeProvider) *mcpconnector.ActionHTTPHandlers {
	return mcpconnector.NewActionHTTPHandlers(scope)
}

func (*Component) NewMCPReadHTTPHandlers(scope mcpconnector.ScopeProvider) *mcpconnector.HTTPHandlers {
	return mcpconnector.NewHTTPHandlers(scope)
}

func (*Component) RuntimeKey(r *http.Request, scope string) string {
	return runtimecontrol.Key(r, scope)
}

func (*Component) NewMCPRuntimeHTTPHandlers(scope runtimecontrol.MCPRuntimeScopeProvider) *runtimecontrol.MCPHTTPHandlers {
	return runtimecontrol.NewMCPHTTPHandlers(scope)
}

func (*Component) NewSecurityHTTPHandlers(scope securitypolicy.HTTPScopeProvider) *securitypolicy.HTTPHandlers {
	return securitypolicy.NewHTTPHandlers(scope)
}

func (*Component) RedactBasic(value string) string {
	return securitypolicy.RedactBasic(value)
}

func (*Component) HashToken(value string) string {
	return tokens.HashToken(value)
}

func (*Component) IsUIExempt(path string) bool {
	return uisession.IsExempt(path)
}

func (*Component) UISessionRetryIdentity(instanceID string) string {
	return uisession.RetryIdentity(instanceID)
}
