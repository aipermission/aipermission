package api

import (
	"context"
	"log"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) accessControlPorts(runtime *gatewayinfra.WorkspaceHandle) gatewayinfra.AccessControlPorts {
	return gatewayinfra.AccessControlPorts{
		FinishTokenInvalidation: func(ctx context.Context, tokenID int64, sessionIDs []int64) {
			lifecycle, err := s.vaultSessionLifecycle(runtime)
			if err == nil {
				err = lifecycle.FinishTokenInvalidation(ctx, tokenID, sessionIDs)
			}
			if err != nil {
				log.Printf("finish token Vault session invalidation failed token=%d sessions=%v error=%v", tokenID, sessionIDs, err)
			}
		},
	}
}
