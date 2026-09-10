// Package filetransferhttp owns the HTTP, queue, runner, and temporary-file
// application surface for connector file transfers.
package filetransferhttp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

type ObservationAudit func(context.Context, string, *int64, int64, string, any)
type ConnectorPorts struct {
	ConnectorKind      string
	Gateway            connectorapi.FileTransferGateway
	Runtime            connectorapi.TransferRuntime
	CredentialBoundary actionresult.CredentialBoundary
}

type ConnectorPortsResolver func(context.Context, int64) (ConnectorPorts, error)

// Runtime is the narrow workspace capability set used by transfer operations.
// The owner of the unlocked workspace retains credential decryption and only
// exposes a redaction boundary to this package.
type Runtime struct {
	store          *filetransfer.Store
	jobs           *transferjobs.Registry
	finalization   transferjobs.FinalizationLifetime
	observe        ObservationAudit
	connectorPorts ConnectorPortsResolver
}

type RuntimeDependencies struct {
	Database       *sql.DB
	Jobs           *transferjobs.Registry
	Finalization   transferjobs.FinalizationLifetime
	Observe        ObservationAudit
	ConnectorPorts ConnectorPortsResolver
}

// NewRuntime creates the transfer-owned workspace surface and store.
func NewRuntime(dependencies RuntimeDependencies) (*Runtime, error) {
	if dependencies.Database == nil {
		return nil, fmt.Errorf("file transfer database is required")
	}
	if dependencies.Jobs == nil {
		return nil, fmt.Errorf("file transfer job registry is required")
	}
	if !dependencies.Finalization.Valid() {
		return nil, fmt.Errorf("file transfer finalization lifetime is required")
	}
	if dependencies.Observe == nil {
		return nil, fmt.Errorf("file transfer observer is required")
	}
	if dependencies.ConnectorPorts == nil {
		return nil, fmt.Errorf("file transfer connector resolver is required")
	}
	return &Runtime{
		store:          filetransfer.NewStore(dependencies.Database),
		jobs:           dependencies.Jobs,
		finalization:   dependencies.Finalization,
		observe:        dependencies.Observe,
		connectorPorts: dependencies.ConnectorPorts,
	}, nil
}

type ScopeProvider func(http.ResponseWriter) (*Runtime, bool)
type AdapterProvider func(string) connectorapi.FileTransferAdapter

type Dependencies struct {
	Scope      ScopeProvider
	AdapterFor AdapterProvider
	DataPath   string
}

type Handlers struct {
	scope      ScopeProvider
	adapterFor AdapterProvider
	dataPath   string
}

func NewHandlers(dependencies Dependencies) *Handlers {
	return &Handlers{
		scope: dependencies.Scope, adapterFor: dependencies.AdapterFor,
		dataPath: dependencies.DataPath,
	}
}

// ShutdownRuntime cancels transfer workers, persists an interrupted terminal
// state for active transfers, and closes their finalization lifetime.
func (h Handlers) ShutdownRuntime(runtime *Runtime, timeout time.Duration, runningMessage string, batchMessage string) (bool, error) {
	if runtime == nil {
		return true, nil
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	drained := runtime.jobs.Shutdown(shutdownCtx)
	cancel()
	err := runtime.store.FailActive(context.Background(), runningMessage, batchMessage)
	runtime.finalization.Stop()
	return drained, err
}

func (h Handlers) activeRuntimeOrLocked(w http.ResponseWriter) (*Runtime, bool) {
	if h.scope == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	runtime, ok := h.scope(w)
	if !ok {
		return nil, false
	}
	if runtime == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	return runtime, true
}

func (h Handlers) connectorFileTransferAdapterFor(kind string) connectorapi.FileTransferAdapter {
	if h.adapterFor == nil {
		return nil
	}
	return h.adapterFor(kind)
}

func (h Handlers) writeObservationAudit(ctx context.Context, runtime *Runtime, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	if runtime != nil {
		runtime.observe(ctx, actor, tokenID, runtimeID, action, payload)
	}
}

type connectorCredentialBoundary = actionresult.CredentialBoundary

func connectorFileTransferPortsForID(ctx context.Context, runtime *Runtime, runtimeID int64) (ConnectorPorts, error) {
	if runtime == nil {
		return ConnectorPorts{}, fmt.Errorf("file transfer runtime is unavailable")
	}
	ports, err := runtime.connectorPorts(ctx, runtimeID)
	if err != nil {
		return ConnectorPorts{}, err
	}
	if ports.ConnectorKind == "" || ports.Gateway == nil || ports.Runtime == nil || !ports.CredentialBoundary.Valid() {
		return ConnectorPorts{}, fmt.Errorf("file transfer connector runtime is unavailable")
	}
	return ports, nil
}

func handleConnectorTargetRuntimeError(w http.ResponseWriter, err error) {
	if errors.Is(err, connectortargets.ErrTargetProfileNotFound) ||
		errors.Is(err, connectortargets.ErrTargetNotFound) ||
		errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) ||
		errors.Is(err, connectortargets.ErrInvalidTargetRef) {
		writeError(w, http.StatusNotFound, "connector target profile not found")
		return
	}
	writeInternalError(w)
}

func writeError(w http.ResponseWriter, status int, message string) {
	httptransport.WriteError(w, status, message)
}

func writeInternalError(w http.ResponseWriter) {
	httptransport.WriteInternalError(w)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	httptransport.WriteJSON(w, status, value)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	return httptransport.DecodeJSON(w, r, target, httptransport.DefaultJSONBodyBytes)
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return httptransport.ParsePathInt64(w, r, "id", "invalid id")
}

func parseInt64Query(w http.ResponseWriter, value, name string) (int64, bool) {
	return httptransport.ParseQueryInt64(w, value, name)
}

type pageRequest = httptransport.PageRequest
type pageResponse[T any] = httptransport.PageResponse[T]

func parsePageRequest(r *http.Request) (pageRequest, error) {
	return httptransport.ParsePageRequest(r)
}

func makePageResponse[T any](items []T, total int, page pageRequest) pageResponse[T] {
	return httptransport.MakePageResponse(items, total, page)
}
