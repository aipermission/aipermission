package api

import (
	"context"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) projectPorts(runtime *gatewayinfra.WorkspaceHandle) gatewayinfra.ProjectPorts {
	return gatewayinfra.ProjectPorts{
		Invalidate: func(ctx context.Context, projectID int64, references []gatewayvault.SessionReference) error {
			lifecycle, err := s.vaultSessionLifecycle(runtime)
			if err != nil {
				return err
			}
			return lifecycle.InvalidateProjectReferences(ctx, projectID, "project was archived; send a fresh Vault request", references)
		},
	}
}
