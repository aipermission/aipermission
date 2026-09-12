package api

import (
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
		LiveConsoleKind: s.connectorRuntime.LiveConsoleCapabilityKind,
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
