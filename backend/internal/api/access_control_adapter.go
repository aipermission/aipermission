package api

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
)

func (s *Server) accessControlScope(w http.ResponseWriter) (accesscontrol.Scope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return accesscontrol.Scope{}, false
	}
	return accesscontrol.Scope{
		Database: runtime.database,
		Tokens:   runtime.tokens,
		Registry: runtime.connectorRegistry(),
		ReusableTokens: func(ctx context.Context) (bool, error) {
			settings, err := runtime.securityPolicy.ReadSettings(ctx)
			return settings.ReusableTokens, err
		},
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
		AcquireExclusive: runtime.vaultDelivery.acquireExclusive,
		FinishTokenInvalidation: func(ctx context.Context, tokenID int64, sessionIDs []int64) {
			if err := s.finishVaultTokenSessionInvalidation(ctx, runtime, tokenID, sessionIDs); err != nil {
				log.Printf("finish token Vault session invalidation failed token=%d sessions=%v error=%v", tokenID, sessionIDs, err)
			}
		},
	}, true
}
