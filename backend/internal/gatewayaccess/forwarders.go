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

func NewCapabilityStore(db *sql.DB) *accesscontrol.CapabilityStore {
	return accesscontrol.NewCapabilityStore(db)
}

func NewAccessHTTPHandlers(scope accesscontrol.ScopeProvider) *accesscontrol.HTTPHandlers {
	return accesscontrol.NewHTTPHandlers(scope)
}

func AnalyzeCommandPolicy(command string) []commandrequests.PolicyWarning {
	return commandrequests.AnalyzePolicy(command)
}

func NewBulkHTTPHandlers(scope commandrequests.BulkHTTPScopeProvider) *commandrequests.BulkHTTPHandlers {
	return commandrequests.NewBulkHTTPHandlers(scope)
}

func NewCommandHTTPHandlers(scope commandrequests.HTTPScopeProvider) *commandrequests.HTTPHandlers {
	return commandrequests.NewHTTPHandlers(scope)
}

func NewCommandWorkspaceRuntime(dependencies commandrequests.WorkspaceRuntimeDependencies) (*commandrequests.Runtime, error) {
	return commandrequests.NewWorkspaceRuntime(dependencies)
}

func NewRuntimeInstanceID() (string, error) {
	return executionprincipal.NewRuntimeInstanceID()
}

func PrincipalLocalOperator(workspaceID string, runtimeInstanceID string) (executionprincipal.Principal, error) {
	return executionprincipal.LocalOperator(workspaceID, runtimeInstanceID)
}

func PrincipalMCPToken(tokenID int64, workspaceID string, runtimeInstanceID string) (executionprincipal.Principal, error) {
	return executionprincipal.MCPToken(tokenID, workspaceID, runtimeInstanceID)
}

func NewMCPActionHTTPHandlers(scope mcpconnector.ActionScopeProvider) *mcpconnector.ActionHTTPHandlers {
	return mcpconnector.NewActionHTTPHandlers(scope)
}

func NewMCPReadHTTPHandlers(scope mcpconnector.ScopeProvider) *mcpconnector.HTTPHandlers {
	return mcpconnector.NewHTTPHandlers(scope)
}

func RuntimeKey(r *http.Request, scope string) string {
	return runtimecontrol.Key(r, scope)
}

func NewMCPRuntimeHTTPHandlers(scope runtimecontrol.MCPRuntimeScopeProvider) *runtimecontrol.MCPHTTPHandlers {
	return runtimecontrol.NewMCPHTTPHandlers(scope)
}

func NewSecurityHTTPHandlers(scope securitypolicy.HTTPScopeProvider) *securitypolicy.HTTPHandlers {
	return securitypolicy.NewHTTPHandlers(scope)
}

func RedactBasic(value string) string {
	return securitypolicy.RedactBasic(value)
}

func HashToken(value string) string {
	return tokens.HashToken(value)
}

func IsUIExempt(path string) bool {
	return uisession.IsExempt(path)
}

func PrepareUISession() (uisession.Prepared, error) {
	return uisession.Prepare()
}

func UISessionRetryIdentity(instanceID string) string {
	return uisession.RetryIdentity(instanceID)
}
