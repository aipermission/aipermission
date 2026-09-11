// Package transfer owns the workspace-scoped file-transfer runtime and
// worker lifetime independently from any transport.
package transfer

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
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

type Storage interface {
	ApproveBatch(context.Context, int64, filetransfer.BatchApprovalRequest) (filetransfer.BatchRecord, []filetransfer.Record, error)
	Cancel(context.Context, int64, string) (bool, error)
	CancelBatch(context.Context, int64, string) (bool, error)
	CreateBatch(context.Context, filetransfer.CreateBatchRequest) (filetransfer.BatchRecord, error)
	CreateBatchIdempotent(context.Context, filetransfer.CreateBatchRequest, filetransfer.IdempotencyClaim) (filetransfer.BatchRecord, bool, error)
	CreateIdempotent(context.Context, filetransfer.CreateRequest, filetransfer.IdempotencyClaim) (filetransfer.Record, bool, error)
	DeclineBatch(context.Context, int64, string) (filetransfer.BatchRecord, []filetransfer.Record, error)
	Get(context.Context, int64) (filetransfer.Record, error)
	GetBatch(context.Context, int64) (filetransfer.BatchRecord, error)
	GetIdempotentBatch(context.Context, filetransfer.IdempotencyClaim) (filetransfer.BatchRecord, error)
	GetIdempotentTransfer(context.Context, filetransfer.IdempotencyClaim) (filetransfer.Record, error)
	List(context.Context, filetransfer.ListFilter) ([]filetransfer.Record, int, error)
	ListBatchItems(context.Context, int64) ([]filetransfer.Record, error)
	ListBatches(context.Context, filetransfer.BatchListFilter) ([]filetransfer.BatchRecord, int, error)
	PauseBatch(context.Context, int64) (bool, error)
	ResumeBatch(context.Context, int64) (bool, error)
	UpdatePausedBatchQueue(context.Context, int64, []int64) ([]filetransfer.Record, error)
}

type BatchControl interface {
	Pause() bool
	Resume() bool
}

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

func (runtime *Runtime) Storage() Storage {
	if runtime == nil {
		return nil
	}
	return runtime.store
}

func (runtime *Runtime) CancelFileJob(id int64) {
	if runtime != nil {
		runtime.jobs.Files.Cancel(id)
	}
}

func (runtime *Runtime) BatchControl(id int64) BatchControl {
	if runtime == nil {
		return nil
	}
	control := runtime.jobs.Batches.Control(id)
	if control == nil {
		return nil
	}
	return control
}

func (runtime *Runtime) CancelBatchJob(id int64) {
	if runtime != nil {
		runtime.jobs.Batches.Cancel(id)
	}
}

func (runtime *Runtime) Observe(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	if runtime != nil && runtime.observe != nil {
		runtime.observe(ctx, actor, tokenID, runtimeID, action, payload)
	}
}

func (runtime *Runtime) ConnectorPorts(ctx context.Context, runtimeID int64) (ConnectorPorts, error) {
	if runtime == nil || runtime.connectorPorts == nil {
		return ConnectorPorts{}, fmt.Errorf("file transfer runtime is unavailable")
	}
	return runtime.connectorPorts(ctx, runtimeID)
}

// Shutdown cancels transfer workers, persists interrupted terminal states,
// and closes the finalization lifetime before workspace storage is released.
func (runtime *Runtime) Shutdown(timeout time.Duration, runningMessage string, batchMessage string) (bool, error) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return runtime.ShutdownContext(shutdownCtx, runningMessage, batchMessage)
}

func (runtime *Runtime) ShutdownContext(ctx context.Context, runningMessage string, batchMessage string) (bool, error) {
	if runtime == nil {
		return true, nil
	}
	drained := runtime.jobs.Shutdown(ctx)
	err := runtime.store.FailActive(context.Background(), runningMessage, batchMessage)
	runtime.finalization.Stop()
	return drained, err
}
