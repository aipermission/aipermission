package api

import (
	"context"
	"database/sql"
	"errors"
	"log"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) vaultRuntime(runtime databaseRuntime) gatewayvault.Runtime {
	if runtime == nil {
		return gatewayvault.Runtime{}
	}
	delivery := runtime.SecurityPort().VaultDeliveryCoordinator()
	return gatewayvault.Runtime{
		Storage: gatewayvault.StorageRuntime{
			Database: runtime.StoragePort().DatabaseHandle(), SecretVault: runtime.StoragePort().SecretVault(),
			Tokens: runtime.StoragePort().TokenStore(), WorkspaceID: runtime.WorkspaceIdentifier(), DatabaseID: runtime.DatabaseIdentifier(),
		},
		Session: gatewayvault.SessionRuntime{
			Sessions: runtime.ConnectorPort().ConsoleSessionManager(), Leases: runtime.SecurityPort().VaultLeaseStore(),
			RuntimeInstanceID: runtime.RuntimeIdentifier(), MCPStarted: runtime.IsMCPStarted,
			AcquireDelivery: delivery.AcquireDelivery, AcquireExclusive: delivery.AcquireExclusive,
		},
		Project: gatewayvault.ProjectRuntimePorts{
			State: runtime.ComponentStatePort(),
			InvalidateSessions: func(ctx context.Context, sessions []gatewayvault.SessionReference, scope gatewayvault.SessionMutationScope) error {
				lifecycle, err := s.vaultSessionLifecycle(runtime)
				if err != nil {
					return err
				}
				return lifecycle.InvalidateMutation(ctx, sessions, scope)
			},
			SessionEnvironment: func(ctx context.Context, runtimeID int64) (bool, error) {
				err := requireSessionEnvironmentCapability(ctx, s, runtime, runtimeID)
				if errors.Is(err, connectors.ErrSessionEnvironmentUnsupported) {
					return false, nil
				}
				return err == nil, err
			},
			Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
				return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
			},
			Observe: func(ctx context.Context, action string, payload any) error {
				return s.writeAuditRequired(ctx, runtime, "user", nil, 0, action, payload)
			},
		},
		Action: gatewayvault.ActionRuntimePorts{
			Connector: vaultActionConnectorPort{server: s, runtime: runtime},
			Mutate: func(ctx context.Context, tokenID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
				return s.withAuditedMutation(ctx, runtime, "mcp", &tokenID, 0, action, payload, mutate)
			},
		},
		Requests: gatewayvault.RequestRuntimePorts{
			Store: s.observation.VaultRequestStoreFactory(observationRuntime(runtime)),
			Mutate: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
				return s.withAuditedMutation(ctx, runtime, actor, tokenID, runtimeID, action, payload, mutate)
			},
			Observe: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
				s.writeObservationAudit(ctx, runtime, actor, tokenID, runtimeID, action, payload)
			},
			RepairProjection: func(ctx context.Context, id int64) error {
				if err := s.observation.SyncVaultActionRequest(ctx, observationRuntime(runtime), id); err != nil {
					log.Printf("Vault request history projection repair failed request=%d error=%v", id, err)
				}
				return nil
			},
			RedactRequestError: func(ctx context.Context, err error) string {
				return s.redactForPersistence(ctx, runtime, err.Error())
			},
		},
	}
}
