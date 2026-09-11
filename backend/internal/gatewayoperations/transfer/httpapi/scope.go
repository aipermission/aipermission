// Package filetransferhttp exposes connector file transfers over HTTP.
package filetransferhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type ScopeProvider func(http.ResponseWriter) (*transferapp.Runtime, bool)
type AdapterProvider func(string) connectorapi.FileTransferAdapter

type Dependencies struct {
	Scope      ScopeProvider
	AdapterFor AdapterProvider
	DataPath   string
}

type Handlers struct {
	scope      ScopeProvider
	adapterFor AdapterProvider
	runner     *transferapp.Runner
}

func NewHandlers(dependencies Dependencies) *Handlers {
	return &Handlers{
		scope: dependencies.Scope, adapterFor: dependencies.AdapterFor,
		runner: transferapp.NewRunner(transferapp.RunnerConfig{
			DataPath: dependencies.DataPath, MaxObjectBytes: maxFileTransferObjectBytes, MaxBatchBytes: maxFileTransferBatchBytes,
			TransferTimeout: fileTransferTimeout, BatchTimeout: fileTransferBatchTimeout,
			TempTTL: fileTransferTempTTL,
		}),
	}
}

func (h Handlers) activeRuntimeOrLocked(w http.ResponseWriter) (*transferapp.Runtime, bool) {
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

func (h Handlers) writeObservationAudit(ctx context.Context, runtime *transferapp.Runtime, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	runtime.Observe(ctx, actor, tokenID, runtimeID, action, payload)
}

type connectorCredentialBoundary = actionresult.CredentialBoundary

func connectorFileTransferPortsForID(ctx context.Context, runtime *transferapp.Runtime, runtimeID int64) (transferapp.ConnectorPorts, error) {
	if runtime == nil {
		return transferapp.ConnectorPorts{}, fmt.Errorf("file transfer runtime is unavailable")
	}
	ports, err := runtime.ConnectorPorts(ctx, runtimeID)
	if err != nil {
		return transferapp.ConnectorPorts{}, err
	}
	if ports.ConnectorKind == "" || ports.Gateway == nil || ports.Runtime == nil || !ports.CredentialBoundary.Valid() {
		return transferapp.ConnectorPorts{}, fmt.Errorf("file transfer connector runtime is unavailable")
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
