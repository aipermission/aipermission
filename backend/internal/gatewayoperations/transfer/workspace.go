package gatewaytransfer

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/runtime"
)

// FileTransferWorkspace is the transfer-owned portion of an unlocked workspace.
type FileTransferWorkspace interface {
	transferapp.Workspace
}

type FileTransferWorkspaceScope func(http.ResponseWriter) (FileTransferWorkspace, bool)

// CredentialBoundary keeps the transfer-owned redaction primitive behind the
// transfer composition contract.
type CredentialBoundary struct {
	value actionresult.CredentialBoundary
}

func NewCredentialBoundary(secrets map[string]any) CredentialBoundary {
	return CredentialBoundary{value: actionresult.NewCredentialBoundary(secrets)}
}

func (boundary CredentialBoundary) Add(values ...string) { boundary.value.Add(values...) }
func (boundary CredentialBoundary) AddStructured(value any) {
	boundary.value.AddStructured(value)
}
func (boundary CredentialBoundary) Valid() bool { return boundary.value.Valid() }

type FileTransferConnectorPorts struct {
	ConnectorKind      string
	Gateway            connectorapi.FileTransferGateway
	Runtime            connectorapi.TransferRuntime
	CredentialBoundary CredentialBoundary
}

type FileTransferConnectorPortsResolver func(context.Context, int64) (FileTransferConnectorPorts, error)

type FileTransferHTTPDependencies struct {
	Scope      FileTransferWorkspaceScope
	AdapterFor FileTransferAdapterProvider
	DataPath   string
}

func InitializeFileTransferWorkspace(
	workspace FileTransferWorkspace,
	database *sql.DB,
	observe transferapp.ObservationAudit,
	resolve FileTransferConnectorPortsResolver,
) error {
	if workspace == nil {
		return fmt.Errorf("file transfer workspace state is unavailable")
	}
	if resolve == nil {
		return fmt.Errorf("file transfer connector resolver is unavailable")
	}
	return transferapp.InitializeWorkspace(workspace, database, observe, func(ctx context.Context, runtimeID int64) (transferapp.ConnectorPorts, error) {
		ports, err := resolve(ctx, runtimeID)
		if err != nil {
			return transferapp.ConnectorPorts{}, err
		}
		return transferapp.ConnectorPorts{
			ConnectorKind: ports.ConnectorKind, Gateway: ports.Gateway, Runtime: ports.Runtime,
			CredentialBoundary: ports.CredentialBoundary.value,
		}, nil
	})
}

func NewFileTransferHTTPHandlers(dependencies FileTransferHTTPDependencies) *FileTransferHTTPHandlers {
	return newFileTransferHTTPHandlers(fileTransferHandlerDependencies{
		Scope: func(w http.ResponseWriter) (*transferapp.Runtime, bool) {
			if dependencies.Scope == nil {
				writeInternalError(w)
				return nil, false
			}
			workspace, ok := dependencies.Scope(w)
			if !ok {
				return nil, false
			}
			if workspace == nil {
				writeInternalError(w)
				return nil, false
			}
			runtime, err := transferapp.RuntimeForWorkspace(workspace)
			if err != nil {
				writeInternalError(w)
				return nil, false
			}
			return runtime, true
		},
		AdapterFor: dependencies.AdapterFor, DataPath: dependencies.DataPath,
	})
}

func (s FileTransferHTTPHandlers) CreateAndLaunchDownloadBatchForWorkspace(
	ctx context.Context,
	workspace FileTransferWorkspace,
	authorization connectorapi.TransferAuthorization,
	runtimeID int64,
	remotePaths []string,
	archiveName string,
	source string,
) (filetransfer.BatchRecord, error) {
	if workspace == nil {
		return filetransfer.BatchRecord{}, fmt.Errorf("file transfer workspace runtime is unavailable")
	}
	runtime, err := transferapp.RuntimeForWorkspace(workspace)
	if err != nil {
		return filetransfer.BatchRecord{}, err
	}
	return s.CreateAndLaunchDownloadBatch(ctx, runtime, authorization, runtimeID, remotePaths, archiveName, source)
}

func FileTransferWorkspaceReady(workspace FileTransferWorkspace) bool {
	return workspace != nil && transferapp.WorkspaceReady(workspace)
}

func StopFileTransferWorkspace(workspace FileTransferWorkspace) {
	transferapp.StopWorkspace(workspace)
}
