package api

import gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"

func (s *Server) localExecutionPrincipal(runtime databaseRuntime) (gatewayaccess.Principal, error) {
	return s.access.LocalPrincipal(runtimeAccessIdentity(runtime))
}

func (s *Server) tokenExecutionPrincipal(runtime databaseRuntime, tokenID int64) (gatewayaccess.Principal, error) {
	return s.access.TokenPrincipal(runtimeAccessIdentity(runtime), tokenID)
}

func runtimeAccessIdentity(runtime databaseRuntime) gatewayaccess.RuntimeIdentity {
	if runtime == nil {
		return gatewayaccess.RuntimeIdentity{}
	}
	return gatewayaccess.RuntimeIdentity{WorkspaceID: runtime.Identity.WorkspaceID, RuntimeID: runtime.Identity.RuntimeID}
}

func ensureRuntimeIdentity(runtime databaseRuntime) error {
	if runtime == nil || !runtime.Identity.Ready() {
		return gatewayaccess.ErrInvalidPrincipal
	}
	return nil
}
