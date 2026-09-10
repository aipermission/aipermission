package api

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/applicationvault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

func (s *Server) vaultRequestHTTPScope(w http.ResponseWriter) (vaultrequests.HTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return vaultrequests.HTTPScope{}, false
	}
	return vaultrequests.HTTPScope{
		MCPStarted: runtime.IsMCPStarted,
		Runtime: func(ctx context.Context) (*vaultrequests.Runtime, error) {
			return s.vaultRequestRuntime(ctx, runtime)
		},
	}, true
}

func (s *Server) vaultRequestRuntime(ctx context.Context, runtime *databaseRuntime) (*vaultrequests.Runtime, error) {
	component := s.vaultApplication()
	s.configureVaultActions(component)
	component.ConfigureRequests(applicationvault.RequestDependencies{
		Store: s.observation.VaultRequestStore,
		Mutate: func(ctx context.Context, runtime *workspaceruntime.Runtime, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, actor, tokenID, runtimeID, action, payload, mutate)
		},
		Observe: func(ctx context.Context, runtime *workspaceruntime.Runtime, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
		},
		AllowRequest: func(runtime *workspaceruntime.Runtime, tokenID int64) bool {
			return s.controlState.VaultRequestLimiter != nil && s.controlState.VaultRequestLimiter.Allow(
				"vault-request:"+runtime.ID+":"+strconv.FormatInt(tokenID, 10),
			)
		},
		RepairProjection: func(ctx context.Context, runtime *workspaceruntime.Runtime, id int64) error {
			if err := s.observation.SyncVaultActionRequest(ctx, runtime, id); err != nil {
				log.Printf("Vault request history projection repair failed request=%d error=%v", id, err)
			}
			return nil
		},
		RedactError: func(ctx context.Context, runtime *workspaceruntime.Runtime, err error) string {
			return s.redactForPersistence(ctx, runtime, err.Error())
		},
	})
	return component.RequestRuntime(ctx, runtime)
}
