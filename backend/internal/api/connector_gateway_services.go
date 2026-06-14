package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
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

// ConnectorTrustStorePath exposes the gateway-owned local trust store path to
// connector adapters that pin external endpoint identity.
func (s *Server) ConnectorTrustStorePath() string {
	return s.connectorTrustStorePath()
}

func (s *Server) ConnectorActiveRuntimeAvailable(w http.ResponseWriter) bool {
	_, ok := s.activeRuntimeOrLocked(w)
	return ok
}

// ConnectorRestartConsoleSession closes a persistent live session and cancels
// its running connector requests.
func (s *Server) ConnectorRestartConsoleSession(ctx context.Context, runtime any, runtimeID int64, runningRequestError string) (connectorapi.ConsoleRestartResult, error) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return connectorapi.ConsoleRestartResult{}, errInvalidConnectorRuntime
	}
	result, err := s.restartServerConsoleSession(ctx, dbRuntime, runtimeID, runningRequestError)
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
func (s *Server) ConnectorFinishActionRequest(ctx context.Context, runtime any, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return connectortargets.ActionRequest{}, errInvalidConnectorRuntime
	}
	return s.finishConnectorActionRequest(ctx, dbRuntime, requestID, status, output, displayText, errorText, hints...)
}

// ConnectorStaleActionRequestsForTarget stales pending action requests for a
// target/profile after connector-owned target lifecycle changes.
func (s connectorTargetHandlers) ConnectorStaleActionRequestsForTarget(ctx context.Context, runtime any, targetID int64, profileID int64, reason string) (int64, error) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return 0, errInvalidConnectorRuntime
	}
	return s.staleConnectorActionRequestsForTarget(ctx, dbRuntime, targetID, profileID, reason)
}

// ConnectorWriteAudit writes a connector lifecycle audit event.
func (s connectorTargetHandlers) ConnectorWriteAudit(ctx context.Context, runtime any, actorType string, tokenID *int64, runtimeID int64, action string, payload any) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return
	}
	s.writeAudit(ctx, dbRuntime, actorType, tokenID, runtimeID, action, payload)
}

// ConnectorServer returns the underlying gateway server for adapter calls that
// need shared gateway services.
func (s connectorTargetHandlers) ConnectorServer() any {
	return s.Server
}

// ConnectorServer returns the underlying gateway server for credential
// resource adapters.
func (s credentialHandlers) ConnectorServer() any {
	return s.Server
}

// ConnectorCreateDownloadBatch creates a file-transfer batch for connector
// adapters that expose remote downloads.
func (s *Server) ConnectorCreateDownloadBatch(ctx context.Context, runtime any, runtimeID int64, remotePaths []string, archiveName string, source string, status string) (filetransfer.BatchRecord, error) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return filetransfer.BatchRecord{}, errInvalidConnectorRuntime
	}
	return fileTransferHandlers{s}.createDownloadBatch(ctx, dbRuntime, runtimeID, remotePaths, archiveName, source, status)
}

// ConnectorRunTransferBatch starts a previously-created transfer batch.
func (s *Server) ConnectorRunTransferBatch(runtime any, batchID int64, overwrite bool) {
	dbRuntime, ok := runtime.(*databaseRuntime)
	if !ok || dbRuntime == nil {
		return
	}
	fileTransferHandlers{s}.runTransferBatch(dbRuntime, batchID, overwrite)
}
