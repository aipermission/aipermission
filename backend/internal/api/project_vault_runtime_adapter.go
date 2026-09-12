package api

import (
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	"net/http"

	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) vaultApplication() *gatewayvault.Component {
	if s == nil || s.vault == nil {
		panic("Vault application is not initialized")
	}
	return s.vault
}

func (s *Server) newVaultApplication() *gatewayvault.Component {
	return gatewayvault.New(gatewayvault.Dependencies{
		LiveConsoleKind: func(kind string) (string, bool) {
			adapter := s.connectorLiveConsoleTargetAdapterFor(kind)
			if adapter == nil {
				return "", false
			}
			return adapter.LiveConsoleCapabilityKind(), true
		},
		AllowGenerate: func(key string) bool {
			return s.access.AllowVaultGenerate(key)
		},
		AllowReveal: func(key string) bool {
			return s.access.AllowVaultReveal(key)
		},
		AllowRequest: func(key string) bool {
			return s.access.AllowVaultRequest(key)
		},
	})
}

func (s *Server) projectVaultRuntime(runtime *gatewayinfra.WorkspaceHandle) (gatewayvault.ProjectVaultApplication, error) {
	return s.vaultApplication().ProjectRuntime(s.vaultRuntime(runtime))
}

func (s *Server) projectVaultHTTPScope(w http.ResponseWriter) (gatewayvault.ProjectVaultHTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return gatewayvault.ProjectVaultHTTPScope{}, false
	}
	owner, err := s.projectVaultRuntime(runtime)
	if err != nil {
		writeInternalError(w)
		return gatewayvault.ProjectVaultHTTPScope{}, false
	}
	return gatewayvault.ProjectVaultHTTPScope{
		Runtime: owner, RuntimeID: runtime.Identity().DatabaseID,
		SessionCatalog: s.vaultApplication().SessionCatalog(s.vaultRuntime(runtime)),
	}, true
}
