package filetransferhttp

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
)

// WorkspaceRuntime is the transfer-owned portion of an unlocked workspace.
type WorkspaceRuntime interface {
	FileTransferRuntime() *transferapp.Runtime
	SetFileTransferRuntime(*transferapp.Runtime)
	FileTransferLifecycle() *transferapp.Lifecycle
}

type WorkspaceScope func(http.ResponseWriter) (WorkspaceRuntime, bool)

type ConnectorPorts struct {
	ConnectorKind      string
	Gateway            connectorapi.FileTransferGateway
	Runtime            connectorapi.TransferRuntime
	CredentialBoundary actionresult.CredentialBoundary
}

type ConnectorPortsResolver func(context.Context, int64) (ConnectorPorts, error)

type WorkspaceDependencies struct {
	Scope      WorkspaceScope
	AdapterFor AdapterProvider
	DataPath   string
}

func InitializeWorkspaceRuntime(
	workspace WorkspaceRuntime,
	database *sql.DB,
	observe transferapp.ObservationAudit,
	resolve ConnectorPortsResolver,
) error {
	if workspace == nil || workspace.FileTransferLifecycle() == nil {
		return fmt.Errorf("file transfer workspace lifecycle is unavailable")
	}
	if resolve == nil {
		return fmt.Errorf("file transfer connector resolver is unavailable")
	}
	runtime, err := workspace.FileTransferLifecycle().NewRuntime(database, observe, func(ctx context.Context, runtimeID int64) (transferapp.ConnectorPorts, error) {
		ports, err := resolve(ctx, runtimeID)
		if err != nil {
			return transferapp.ConnectorPorts{}, err
		}
		return transferapp.ConnectorPorts{
			ConnectorKind: ports.ConnectorKind, Gateway: ports.Gateway, Runtime: ports.Runtime,
			CredentialBoundary: ports.CredentialBoundary,
		}, nil
	})
	if err != nil {
		return err
	}
	workspace.SetFileTransferRuntime(runtime)
	return nil
}

func NewWorkspaceHandlers(dependencies WorkspaceDependencies) *Handlers {
	return NewHandlers(Dependencies{
		Scope: func(w http.ResponseWriter) (*transferapp.Runtime, bool) {
			if dependencies.Scope == nil {
				writeInternalError(w)
				return nil, false
			}
			workspace, ok := dependencies.Scope(w)
			if !ok {
				return nil, false
			}
			if workspace == nil || workspace.FileTransferRuntime() == nil {
				writeInternalError(w)
				return nil, false
			}
			return workspace.FileTransferRuntime(), true
		},
		AdapterFor: dependencies.AdapterFor, DataPath: dependencies.DataPath,
	})
}

func (s Handlers) CreateAndLaunchDownloadBatchForWorkspace(
	ctx context.Context,
	workspace WorkspaceRuntime,
	authorization connectorapi.TransferAuthorization,
	runtimeID int64,
	remotePaths []string,
	archiveName string,
	source string,
) (filetransfer.BatchRecord, error) {
	if workspace == nil || workspace.FileTransferRuntime() == nil {
		return filetransfer.BatchRecord{}, fmt.Errorf("file transfer workspace runtime is unavailable")
	}
	return s.CreateAndLaunchDownloadBatch(ctx, workspace.FileTransferRuntime(), authorization, runtimeID, remotePaths, archiveName, source)
}
