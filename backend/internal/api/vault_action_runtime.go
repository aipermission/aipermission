package api

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strconv"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) vaultRequestHTTPScope(w http.ResponseWriter) (gatewayvault.VaultApprovalHTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return gatewayvault.VaultApprovalHTTPScope{}, false
	}
	return gatewayvault.VaultApprovalHTTPScope{
		MCPStarted: runtime.IsMCPStarted,
		Runtime: func(ctx context.Context) (*gatewayvault.VaultRequestRuntime, error) {
			return s.vaultRequestRuntime(ctx, runtime)
		},
	}, true
}

func (s *Server) vaultRequestRuntime(ctx context.Context, runtime databaseRuntime) (*gatewayvault.VaultRequestRuntime, error) {
	component := s.vaultApplication()
	s.configureVaultActions(component)
	component.ConfigureRequests(gatewayvault.RequestDependencies{
		Store: s.observation.VaultRequestStore,
		Mutate: func(ctx context.Context, runtime gatewayinfra.Runtime, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, actor, tokenID, runtimeID, action, payload, mutate)
		},
		Observe: func(ctx context.Context, runtime gatewayinfra.Runtime, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
		},
		AllowRequest: func(runtime gatewayinfra.Runtime, tokenID int64) bool {
			return s.controlState.VaultRequestLimiter != nil && s.controlState.VaultRequestLimiter.Allow(
				"vault-request:"+runtime.DatabaseIdentifier()+":"+strconv.FormatInt(tokenID, 10),
			)
		},
		RepairProjection: func(ctx context.Context, runtime gatewayinfra.Runtime, id int64) error {
			if err := s.observation.SyncVaultActionRequest(ctx, runtime, id); err != nil {
				log.Printf("Vault request history projection repair failed request=%d error=%v", id, err)
			}
			return nil
		},
		RedactError: func(ctx context.Context, runtime gatewayinfra.Runtime, err error) string {
			return s.redactForPersistence(ctx, runtime, err.Error())
		},
	})
	return component.RequestRuntime(ctx, runtime)
}
