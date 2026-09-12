package api

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) accessControlScope(w http.ResponseWriter) (gatewayaccess.AccessScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return gatewayaccess.AccessScope{}, false
	}
	return s.infrastructure.AccessControlWorkspace(runtime, gatewayinfra.AccessControlPorts{
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
		FinishTokenInvalidation: func(ctx context.Context, tokenID int64, sessionIDs []int64) {
			lifecycle, err := s.vaultSessionLifecycle(runtime)
			if err == nil {
				err = lifecycle.FinishTokenInvalidation(ctx, tokenID, sessionIDs)
			}
			if err != nil {
				log.Printf("finish token Vault session invalidation failed token=%d sessions=%v error=%v", tokenID, sessionIDs, err)
			}
		},
	})
}
