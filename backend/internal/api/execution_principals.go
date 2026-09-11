package api

import gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"

func (s *Server) localExecutionPrincipal(runtime databaseRuntime) (gatewayaccess.Principal, error) {
	if err := ensureRuntimeIdentity(runtime); err != nil {
		return gatewayaccess.Principal{}, err
	}
	return s.access.PrincipalLocalOperator(runtime.WorkspaceIdentifier(), runtime.RuntimeIdentifier())
}

func (s *Server) tokenExecutionPrincipal(runtime databaseRuntime, tokenID int64) (gatewayaccess.Principal, error) {
	if err := ensureRuntimeIdentity(runtime); err != nil {
		return gatewayaccess.Principal{}, err
	}
	return s.access.PrincipalMCPToken(tokenID, runtime.WorkspaceIdentifier(), runtime.RuntimeIdentifier())
}

func ensureRuntimeIdentity(runtime databaseRuntime) error {
	if runtime == nil || !runtime.IdentityReady() {
		return gatewayaccess.ErrInvalidPrincipal
	}
	return nil
}
