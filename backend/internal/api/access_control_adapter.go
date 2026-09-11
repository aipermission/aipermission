package api

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

func (s *Server) accessControlScope(w http.ResponseWriter) (gatewayaccess.AccessScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return gatewayaccess.AccessScope{}, false
	}
	return gatewayaccess.AccessScope{
		Database: runtime.StoragePort().DatabaseHandle(),
		Tokens:   runtime.StoragePort().TokenStore(),
		Registry: runtimeConnectorRegistry(runtime),
		ReusableTokens: func(ctx context.Context) (bool, error) {
			settings, err := runtime.SecurityPort().PolicyService().ReadSettings(ctx)
			return settings.ReusableTokens, err
		},
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
		AcquireExclusive: runtime.SecurityPort().VaultDeliveryCoordinator().AcquireExclusive,
		FinishTokenInvalidation: func(ctx context.Context, tokenID int64, sessionIDs []int64) {
			if err := s.finishVaultTokenSessionInvalidation(ctx, runtime, tokenID, sessionIDs); err != nil {
				log.Printf("finish token Vault session invalidation failed token=%d sessions=%v error=%v", tokenID, sessionIDs, err)
			}
		},
	}, true
}
