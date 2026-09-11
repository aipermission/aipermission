package gatewayaccess

import "github.com/aipermission/aipermission/backend/internal/executionprincipal"

type RuntimeIdentity struct {
	WorkspaceID string
	RuntimeID   string
}

func (identity RuntimeIdentity) Ready() bool {
	return identity.WorkspaceID != "" && identity.RuntimeID != ""
}

func (*Component) LocalPrincipal(identity RuntimeIdentity) (Principal, error) {
	if !identity.Ready() {
		return Principal{}, InvalidPrincipalError()
	}
	return executionprincipal.LocalOperator(identity.WorkspaceID, identity.RuntimeID)
}

func (*Component) TokenPrincipal(identity RuntimeIdentity, tokenID int64) (Principal, error) {
	if !identity.Ready() {
		return Principal{}, InvalidPrincipalError()
	}
	return executionprincipal.MCPToken(tokenID, identity.WorkspaceID, identity.RuntimeID)
}
