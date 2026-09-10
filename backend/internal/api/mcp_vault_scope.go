package api

import (
	"context"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

func (s mcpHandlers) mcpVaultScope(w http.ResponseWriter, r *http.Request) (vaultrequests.MCPHTTPScope, bool) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return vaultrequests.MCPHTTPScope{}, false
	}
	return vaultrequests.MCPHTTPScope{
		Database: auth.runtime.database, Vault: auth.runtime.vault, WorkspaceUUID: auth.runtime.workspaceUUID,
		TokenID: auth.TokenID, MCPStarted: auth.runtime.isMCPStarted,
		Runtime: func(ctx context.Context) (*vaultrequests.Runtime, error) {
			return s.vaultRequestRuntime(ctx, auth.runtime)
		},
		MetadataRead: func(ctx context.Context, projectID int64) (bool, error) {
			capability, err := accesscontrol.NewCapabilityStore(auth.runtime.database).Effective(
				ctx, auth.TokenID, projectID, accesscontrol.VaultMetadataRead, time.Now(),
			)
			return err == nil && capability.ExecutionRule == accesscontrol.RuleAlwaysRun, err
		},
	}, true
}
