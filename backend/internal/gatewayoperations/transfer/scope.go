// Package gatewaytransfer owns the file-transfer application and HTTP boundary
// for unlocked workspaces.
package gatewaytransfer

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/runtime"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type fileTransferScopeProvider func(http.ResponseWriter) (*transferapp.Runtime, bool)
type FileTransferAdapterProvider func(string) connectorapi.FileTransferAdapter

type fileTransferHandlerDependencies struct {
	Scope      fileTransferScopeProvider
	AdapterFor FileTransferAdapterProvider
	DataPath   string
}

type FileTransferHTTPHandlers struct {
	scope      fileTransferScopeProvider
	adapterFor FileTransferAdapterProvider
	runner     *transferapp.Runner
}

func newFileTransferHTTPHandlers(dependencies fileTransferHandlerDependencies) *FileTransferHTTPHandlers {
	return &FileTransferHTTPHandlers{
		scope: dependencies.Scope, adapterFor: dependencies.AdapterFor,
		runner: transferapp.NewRunner(transferapp.RunnerConfig{
			DataPath: dependencies.DataPath, MaxObjectBytes: maxFileTransferObjectBytes, MaxBatchBytes: maxFileTransferBatchBytes,
			TransferTimeout: fileTransferTimeout, BatchTimeout: fileTransferBatchTimeout,
			TempTTL: fileTransferTempTTL, AdapterFor: dependencies.AdapterFor,
		}),
	}
}

func (h FileTransferHTTPHandlers) activeRuntimeOrLocked(w http.ResponseWriter) (*transferapp.Runtime, bool) {
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

func (h FileTransferHTTPHandlers) connectorFileTransferAdapterFor(kind string) connectorapi.FileTransferAdapter {
	if h.adapterFor == nil {
		return nil
	}
	return h.adapterFor(kind)
}

func (h FileTransferHTTPHandlers) writeObservationAudit(ctx context.Context, runtime *transferapp.Runtime, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	runtime.Observe(ctx, actor, tokenID, runtimeID, action, payload)
}

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

func parsePageRequest(r *http.Request) (httptransport.PageRequest, error) {
	return httptransport.ParsePageRequest(r)
}

func makePageResponse[T any](items []T, total int, page httptransport.PageRequest) httptransport.PageResponse[T] {
	return httptransport.MakePageResponse(items, total, page)
}
