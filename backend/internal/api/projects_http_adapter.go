package api

import (
	"context"
	"database/sql"
	"net/http"

	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

func (s *Server) projectsHTTPScope(w http.ResponseWriter) (projectstore.Scope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return projectstore.Scope{}, false
	}
	return projectstore.Scope{
		Database: runtime.database,
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
		AcquireExclusive: runtime.vaultDelivery.AcquireExclusive,
		Invalidate: func(ctx context.Context, projectID int64) error {
			return s.invalidateVaultProjectSessions(ctx, runtime, projectID, "project was archived; send a fresh Vault request")
		},
	}, true
}
