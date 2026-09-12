package api

import (
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) configureVaultSessionRuntime(runtime *gatewayinfra.WorkspaceHandle) error {
	owner, err := s.vaultSessionLifecycle(runtime)
	if err != nil {
		return err
	}
	return owner.Configure()
}

func (s *Server) vaultSessionLifecycle(runtime *gatewayinfra.WorkspaceHandle) (*gatewayvault.SessionLifecycle, error) {
	if s == nil || runtime == nil {
		return nil, gatewayvault.InvalidatorUnavailableError()
	}
	return s.vaultOwner.VaultSessionLifecycle(runtime, s.vaultApplication(), s.vaultRuntimePorts(runtime), gatewayinfra.VaultSessionPorts{
		Principal: func() (gatewayaccess.Principal, error) {
			return s.localExecutionPrincipal(runtime)
		},
	})
}
