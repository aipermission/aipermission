package api

import (
	"context"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) projectPorts(runtime *gatewayinfra.WorkspaceHandle) gatewayinfra.ProjectPorts {
	return gatewayinfra.ProjectPorts{
		Invalidate: func(ctx context.Context, projectID int64) error {
			lifecycle, err := s.vaultSessionLifecycle(runtime)
			if err != nil {
				return err
			}
			return lifecycle.InvalidateProject(ctx, projectID, "project was archived; send a fresh Vault request")
		},
	}
}
