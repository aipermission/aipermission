package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

var errFileTransferTimedOut = errors.New("file transfer timed out")

func (s fileTransferHandlers) failFileTransfer(runtime *databaseRuntime, transferID int64, err error, failureKind string) {
	message := fileTransferFailureMessage(err)
	item, readErr := runtime.fileTransfers.Get(context.Background(), transferID)
	if readErr != nil {
		message = "file transfer failed"
	} else if boundary, boundaryErr := connectorCredentialBoundaryForRuntimeID(context.Background(), runtime, item.RuntimeID); boundaryErr != nil {
		message = "file transfer failed"
	} else {
		message = boundary.Redact(message)
	}
	s.persistFileTransferTerminal(runtime, transferID, func(ctx context.Context) (bool, error) {
		return runtime.fileTransfers.FailWithKind(ctx, transferID, message, failureKind)
	})
}

func (s fileTransferHandlers) finishFileTransferError(runtime *databaseRuntime, transferID int64, ctx context.Context, err error) {
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		s.failFileTransfer(runtime, transferID, err, filetransfer.FailureKindOutcomeUnknown)
		return
	}
	switch classifyFileTransferInterruption(ctx, err) {
	case fileTransferTimedOut:
		s.failFileTransfer(runtime, transferID, errFileTransferTimedOut, filetransfer.FailureKindTimeout)
	case fileTransferCanceledByUser:
		s.cancelFileTransferRecord(runtime, transferID, "canceled by local user")
	default:
		s.failFileTransfer(runtime, transferID, err, filetransfer.FailureKindUnknown)
	}
}

func (s fileTransferHandlers) finishFileTransferBatchError(runtime *databaseRuntime, batchID int64, ctx context.Context, err error) bool {
	message := fileTransferFailureMessage(err)
	batch, readErr := runtime.fileTransfers.GetBatch(context.Background(), batchID)
	if readErr != nil {
		message = "file transfer batch failed"
	} else if boundary, boundaryErr := connectorCredentialBoundaryForRuntimeID(context.Background(), runtime, batch.RuntimeID); boundaryErr != nil {
		message = "file transfer batch failed"
	} else {
		message = boundary.Redact(message)
	}
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		return s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
			return runtime.fileTransfers.FailBatchWithKind(ctx, batchID, message, filetransfer.FailureKindOutcomeUnknown)
		})
	}
	switch classifyFileTransferInterruption(ctx, err) {
	case fileTransferTimedOut:
		return s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
			return runtime.fileTransfers.FailBatchWithKind(ctx, batchID, "file transfer batch timed out", filetransfer.FailureKindTimeout)
		})
	case fileTransferCanceledByUser:
		return s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
			return runtime.fileTransfers.CancelBatch(ctx, batchID, "canceled by local user")
		})
	default:
		return s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
			return runtime.fileTransfers.FailBatchWithKind(ctx, batchID, message, filetransfer.FailureKindUnknown)
		})
	}
}

type fileTransferInterruption int

const (
	fileTransferNotInterrupted fileTransferInterruption = iota
	fileTransferCanceledByUser
	fileTransferTimedOut
)

func classifyFileTransferInterruption(ctx context.Context, err error) fileTransferInterruption {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fileTransferTimedOut
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return fileTransferCanceledByUser
	}
	return fileTransferNotInterrupted
}

func (s fileTransferHandlers) cancelFileTransferRecord(runtime *databaseRuntime, transferID int64, message string) {
	s.persistFileTransferTerminal(runtime, transferID, func(ctx context.Context) (bool, error) {
		return runtime.fileTransfers.Cancel(ctx, transferID, message)
	})
}

func (s fileTransferHandlers) persistFileTransferTerminal(runtime *databaseRuntime, transferID int64, persist func(context.Context) (bool, error)) bool {
	return transferjobs.PersistTerminal(runtime.finalization.Context(), "file transfer", transferID, persist, func(ctx context.Context) (string, error) {
		item, err := runtime.fileTransfers.Get(ctx, transferID)
		return item.Status, err
	})
}

func (s fileTransferHandlers) persistFileTransferBatchTerminal(runtime *databaseRuntime, batchID int64, persist func(context.Context) (bool, error)) bool {
	return transferjobs.PersistTerminal(runtime.finalization.Context(), "file transfer batch", batchID, persist, func(ctx context.Context) (string, error) {
		item, err := runtime.fileTransfers.GetBatch(ctx, batchID)
		return item.Status, err
	})
}

func fileTransferFailureMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errFileTransferTimedOut) {
		return errFileTransferTimedOut.Error()
	}
	return fmt.Sprintf("file transfer failed: %v", err)
}
