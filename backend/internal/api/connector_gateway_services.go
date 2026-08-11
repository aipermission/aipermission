package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"sort"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

var errInvalidConnectorRuntime = errors.New("invalid connector runtime")

func (s *Server) connectorTrustStorePath() string {
	return filepath.Join(filepath.Dir(s.config.DataPath), "connector_trust_store")
}

// ConnectorDatabase exposes the unlocked database to connector-owned gateway
// adapters without making the generic API package import connector packages.
func (runtime *databaseRuntime) ConnectorDatabase() *sql.DB {
	if runtime == nil {
		return nil
	}
	return runtime.database
}

// ConnectorVault exposes the unlocked vault to connector-owned gateway
// adapters that manage connector-specific encrypted resources.
func (runtime *databaseRuntime) ConnectorVault() *vault.Vault {
	if runtime == nil {
		return nil
	}
	return runtime.vault
}

// ConnectorResource returns one connector-owned runtime resource.
func (runtime *databaseRuntime) ConnectorResource(kind string, name string) any {
	if runtime == nil {
		return nil
	}
	return runtime.connectorResources[kind+"/"+name]
}

// ConnectorConsoleSessions returns the persistent live session manager used by
// runtime-capable connector adapters.
func (runtime *databaseRuntime) ConnectorConsoleSessions() *console.Manager {
	if runtime == nil {
		return nil
	}
	return runtime.consoleSessions
}

func (runtime *databaseRuntime) ConnectorLocalExecutionPrincipal() (executionprincipal.Principal, error) {
	return localExecutionPrincipal(runtime)
}

// ConnectorTrustStorePath exposes the gateway-owned local trust store path to
// connector adapters that pin external endpoint identity.
func (s *Server) ConnectorTrustStorePath() string {
	return s.connectorTrustStorePath()
}

func (s *Server) ConnectorChangeVaultPeerTrust(ctx context.Context, change func() error) error {
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
		release, err := runtime.vaultDelivery.acquire(ctx)
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

func (s *Server) ConnectorActiveRuntimeAvailable(w http.ResponseWriter) bool {
	_, ok := s.activeRuntimeOrLocked(w)
	return ok
}

func (s *Server) ConnectorRuntimeCapabilities(kind string, runtime connectorapi.GatewayRuntime) connectors.RuntimeCapabilityResolver {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return nil
	}
	return connectorRuntimeCapabilitiesFor(kind, s, dbRuntime)
}

// ConnectorRestartConsoleSession closes a persistent live session and cancels
// its running connector requests.
func (s *Server) ConnectorRestartConsoleSession(ctx context.Context, runtime connectorapi.GatewayRuntime, principal executionprincipal.Principal, runtimeID int64, runningRequestError string) (connectorapi.ConsoleRestartResult, error) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return connectorapi.ConsoleRestartResult{}, errInvalidConnectorRuntime
	}
	result, err := s.restartServerConsoleSession(ctx, dbRuntime, principal, runtimeID, runningRequestError)
	if err != nil {
		return connectorapi.ConsoleRestartResult{}, err
	}
	return connectorapi.ConsoleRestartResult{
		ClosedSessionIDs:        result.ClosedSessionIDs,
		CanceledRunningRequests: result.CanceledRunningRequests,
	}, nil
}

// ConnectorFinishActionRequest lets a runtime adapter finish an asynchronous
// connector request after background execution completes.
func (s *Server) ConnectorFinishActionRequest(ctx context.Context, runtime connectorapi.GatewayRuntime, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return connectortargets.ActionRequest{}, errInvalidConnectorRuntime
	}
	return s.finishConnectorActionRequest(ctx, dbRuntime, requestID, status, output, displayText, errorText, hints...)
}

// ConnectorStaleActionRequestsForTarget stales pending action requests for a
// target/profile after connector-owned target lifecycle changes.
func (s connectorTargetHandlers) ConnectorStaleActionRequestsForTarget(ctx context.Context, runtime connectorapi.GatewayRuntime, targetID int64, profileID int64, reason string) (int64, error) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return 0, errInvalidConnectorRuntime
	}
	return s.staleConnectorActionRequestsForTarget(ctx, dbRuntime, targetID, profileID, reason, false)
}

// ConnectorWriteAudit writes a connector lifecycle audit event.
func (s connectorTargetHandlers) ConnectorWriteAudit(ctx context.Context, runtime connectorapi.GatewayRuntime, actorType string, tokenID *int64, runtimeID int64, action string, payload any) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return
	}
	s.writeObservationAudit(ctx, dbRuntime, actorType, tokenID, runtimeID, action, payload)
}

// ConnectorDeleteTargetRecord atomically deletes a connector target and
// records its shared lifecycle audit event. Connector-owned adapters perform
// remote cleanup before crossing this irreversible local boundary.
func (s connectorTargetHandlers) ConnectorDeleteTargetRecord(ctx context.Context, runtime connectorapi.GatewayRuntime, target connectortargets.Target, payload map[string]any) error {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return errInvalidConnectorRuntime
	}
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
func (s connectorTargetHandlers) ConnectorFinalizeDeletedTarget(ctx context.Context, runtime connectorapi.GatewayRuntime, target connectortargets.Target, staleReason string, payload map[string]any) (int64, error) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return 0, errInvalidConnectorRuntime
	}
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
	staleRequests, err := s.staleConnectorActionRequestsForTarget(ctx, dbRuntime, target.ID, 0, staleReason, true)
	if err != nil {
		return 0, err
	}
	return staleRequests, nil
}

// ConnectorServer returns the underlying gateway server for adapter calls that
// need shared gateway services.
func (s connectorTargetHandlers) ConnectorServer() connectorapi.GatewayServer {
	return s.Server
}

// ConnectorServer returns the underlying gateway server for credential
// resource adapters.
func (s credentialHandlers) ConnectorServer() connectorapi.GatewayServer {
	return s.Server
}

// ConnectorCreateDownloadBatch creates a file-transfer batch for connector
// adapters that expose remote downloads.
func (s *Server) ConnectorCreateDownloadBatch(ctx context.Context, runtime connectorapi.GatewayRuntime, runtimeID int64, remotePaths []string, archiveName string, source string, status string) (filetransfer.BatchRecord, error) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return filetransfer.BatchRecord{}, errInvalidConnectorRuntime
	}
	return fileTransferHandlers{s}.createDownloadBatch(ctx, dbRuntime, runtimeID, remotePaths, archiveName, source, status)
}

// ConnectorRunTransferBatch starts a previously-created transfer batch.
func (s *Server) ConnectorRunTransferBatch(runtime connectorapi.GatewayRuntime, batchID int64, overwrite bool) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return
	}
	fileTransferHandlers{s}.runTransferBatch(dbRuntime, batchID, overwrite)
}
