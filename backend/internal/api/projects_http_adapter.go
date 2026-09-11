package api

import (
	"context"
	"database/sql"
	"net/http"

	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) projectsHTTPScope(w http.ResponseWriter) (gatewayvault.ProjectScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return gatewayvault.ProjectScope{}, false
	}
	return gatewayvault.ProjectScope{
		Database: runtime.Storage.DatabaseHandle(),
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
		AcquireExclusive: runtime.Security.VaultDeliveryCoordinator().AcquireExclusive,
		Invalidate: func(ctx context.Context, projectID int64) error {
			lifecycle, err := s.vaultSessionLifecycle(runtime)
			if err != nil {
				return err
			}
			return lifecycle.InvalidateProject(ctx, projectID, "project was archived; send a fresh Vault request")
		},
	}, true
}
