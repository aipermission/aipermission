package api

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sort"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

var errInvalidConnectorRuntime = errors.New("invalid connector runtime")

func (s *Server) connectorTrustStorePath() string {
	return filepath.Join(filepath.Dir(s.config.DataPath), "connector_trust_store")
}

// ConnectorTrustStorePath exposes the gateway-owned local trust store path to
// connector adapters that pin external endpoint identity.
func (s *Server) ConnectorTrustStorePath() string {
	return s.connectorTrustStorePath()
}

func (s *Server) connectorChangeVaultPeerTrust(ctx context.Context, change func() error) error {
	if change == nil {
		return errors.New("connector peer trust change is required")
	}
	runtimes := s.unlockedRuntimeSnapshot()
	if len(runtimes) == 0 {
		return errors.New("database is locked")
	}
	sort.Slice(runtimes, func(i, j int) bool {
		return runtimes[i].id < runtimes[j].id
	})
	releases := make([]func(), 0, len(runtimes))
	for _, runtime := range runtimes {
		release, err := runtime.vaultDelivery.acquireExclusive(ctx)
		if err != nil {
			for index := len(releases) - 1; index >= 0; index-- {
				releases[index]()
			}
			return err
		}
		releases = append(releases, release)
	}
	defer func() {
		for index := len(releases) - 1; index >= 0; index-- {
			releases[index]()
		}
	}()
	for _, runtime := range runtimes {
		runtimeIDs, err := vaultAllRuntimeIDs(ctx, runtime)
		if err != nil {
			return err
		}
		runtime.vaultLeases.Clear()
		if err := revokeAllPersistedVaultLeases(ctx, runtime); err != nil {
			return err
		}
		if err := s.invalidateVaultRuntimeSessions(
			ctx,
			runtime,
			runtimeIDs,
			"connector peer trust changed; send a fresh Vault request",
		); err != nil {
			return err
		}
	}
	return change()
}

// ConnectorDeleteTargetRecord atomically deletes a connector target and
// records its shared lifecycle audit event. Connector-owned adapters perform
// remote cleanup before crossing this irreversible local boundary.
func (s connectorTargetHandlers) connectorDeleteTargetRecord(ctx context.Context, dbRuntime *databaseRuntime, target connectortargets.Target, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["target_id"] = target.ID
	payload["connector_kind"] = target.ConnectorKind
	payload["name"] = target.Name
	return s.withAuditedMutation(
		ctx, dbRuntime, "user", nil, 0, "connector.target.deleted",
		func() any { return payload },
		func(tx *sql.Tx) error { return connectortargets.NewTxStore(tx).DeleteTarget(ctx, target.ID) },
	)
}

// ConnectorFinalizeDeletedTarget applies the shared post-delete lifecycle:
// pending connector action requests are marked stale after the target record
// and its audit event commit atomically.
func (s connectorTargetHandlers) connectorFinalizeDeletedTarget(ctx context.Context, dbRuntime *databaseRuntime, target connectortargets.Target, staleReason string, payload map[string]any) (int64, error) {
	if staleReason == "" {
		staleReason = "connector target was deleted; ask the AI to send a fresh request"
	}
	if err := s.invalidateVaultSessionsForTargetProfile(
		ctx,
		dbRuntime,
		target.ID,
		0,
		"connector target was deleted; send a fresh Vault request",
	); err != nil {
		return 0, err
	}
	staleRequests, err := s.invalidateConnectorActionRequestsForTarget(ctx, dbRuntime, target.ID, 0, staleReason, true)
	if err != nil {
		return 0, err
	}
	return staleRequests, nil
}
