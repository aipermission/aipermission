package gatewayaccess

import "github.com/aipermission/aipermission/backend/internal/executionprincipal"

type RuntimeIdentity interface {
	WorkspaceIdentifier() string
	RuntimeIdentifier() string
	IdentityReady() bool
}

func (*Component) LocalPrincipal(runtime RuntimeIdentity) (Principal, error) {
	if runtime == nil || !runtime.IdentityReady() {
		return Principal{}, ErrInvalidPrincipal
	}
	return executionprincipal.LocalOperator(runtime.WorkspaceIdentifier(), runtime.RuntimeIdentifier())
}

func (*Component) TokenPrincipal(runtime RuntimeIdentity, tokenID int64) (Principal, error) {
	if runtime == nil || !runtime.IdentityReady() {
		return Principal{}, ErrInvalidPrincipal
	}
	return executionprincipal.MCPToken(tokenID, runtime.WorkspaceIdentifier(), runtime.RuntimeIdentifier())
}
