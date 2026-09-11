package gatewayaccess

import (
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/uisession"
)

func (*Component) NewCapabilityStore(db *sql.DB) *accesscontrol.CapabilityStore {
	return accesscontrol.NewCapabilityStore(db)
}

func (*Component) PrincipalLocalOperator(workspaceID string, runtimeInstanceID string) (executionprincipal.Principal, error) {
	return executionprincipal.LocalOperator(workspaceID, runtimeInstanceID)
}

func (*Component) PrincipalMCPToken(tokenID int64, workspaceID string, runtimeInstanceID string) (executionprincipal.Principal, error) {
	return executionprincipal.MCPToken(tokenID, workspaceID, runtimeInstanceID)
}

func (*Component) RuntimeKey(r *http.Request, scope string) string {
	return runtimecontrol.Key(r, scope)
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
