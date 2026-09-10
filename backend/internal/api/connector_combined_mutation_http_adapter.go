package api

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
)

func (s *Server) connectorCombinedMutationHTTPScope(w http.ResponseWriter) (connectormanagement.CombinedMutationScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return connectormanagement.CombinedMutationScope{}, false
	}
	target := s.connectorTargetMutationScope(runtime)
	profile := s.connectorProfileMutationScope(runtime)
	return connectormanagement.CombinedMutationScope{
		Database: target.Database, Registry: target.Registry,
		Preparation: profile.Preparation, ValidateTransport: target.ValidateTransport,
		AcquireExclusive: target.AcquireExclusive, WithTransaction: profile.WithTransaction,
		BeforeCreate: profile.BeforeCreate, EnsureRuntimeSurfaces: profile.EnsureRuntimeSurfaces,
		AfterLifecycleChange: profile.AfterLifecycleChange,
	}, true
}
