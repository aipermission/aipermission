package api

import gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"

import gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"

func (s *Server) localExecutionPrincipal(runtime *gatewayinfra.WorkspaceHandle) (gatewayaccess.Principal, error) {
	return s.access.LocalPrincipal(runtimeAccessIdentity(runtime))
}

func (s *Server) tokenExecutionPrincipal(runtime *gatewayinfra.WorkspaceHandle, tokenID int64) (gatewayaccess.Principal, error) {
	return s.access.TokenPrincipal(runtimeAccessIdentity(runtime), tokenID)
}

func runtimeAccessIdentity(runtime *gatewayinfra.WorkspaceHandle) gatewayaccess.RuntimeIdentity {
	if runtime == nil {
		return gatewayaccess.RuntimeIdentity{}
	}
	return gatewayaccess.RuntimeIdentity{WorkspaceID: runtime.Identity().WorkspaceID, RuntimeID: runtime.Identity().RuntimeID}
}

func ensureRuntimeIdentity(runtime *gatewayinfra.WorkspaceHandle) error {
	if runtime == nil || !runtime.Identity().Ready() {
		return gatewayaccess.InvalidPrincipalError()
	}
	return nil
}
