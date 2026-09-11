package api

import (
	"net/http"

	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) vaultApplication() *gatewayvault.Component {
	return gatewayvault.New(gatewayvault.Dependencies{
		LiveConsoleKind: func(kind string) (string, bool) {
			adapter := s.connectorLiveConsoleTargetAdapterFor(kind)
			if adapter == nil {
				return "", false
			}
			return adapter.LiveConsoleCapabilityKind(), true
		},
		AllowGenerate: func(key string) bool {
			return s.infrastructure.AllowVaultGenerate(key)
		},
		AllowReveal: func(key string) bool {
			return s.infrastructure.AllowVaultReveal(key)
		},
	})
}

func (s *Server) projectVaultRuntime(runtime databaseRuntime) (*gatewayvault.ProjectVaultRuntime, error) {
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
		Runtime: owner, RuntimeID: runtime.DatabaseIdentifier(),
		SessionCatalog: s.vaultApplication().SessionCatalog(s.vaultRuntime(runtime)),
	}, true
}
