package api

import gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"

func (s *Server) localExecutionPrincipal(runtime databaseRuntime) (gatewayaccess.Principal, error) {
	return s.access.LocalPrincipal(runtime)
}

func (s *Server) tokenExecutionPrincipal(runtime databaseRuntime, tokenID int64) (gatewayaccess.Principal, error) {
	return s.access.TokenPrincipal(runtime, tokenID)
}

func ensureRuntimeIdentity(runtime databaseRuntime) error {
	if runtime == nil || !runtime.IdentityReady() {
		return gatewayaccess.ErrInvalidPrincipal
	}
	return nil
}
