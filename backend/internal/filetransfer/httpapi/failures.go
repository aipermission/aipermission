package filetransferhttp

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

var errFileTransferTimedOut = errors.New("file transfer timed out")

func (s Handlers) failFileTransfer(runtime *Runtime, transferID int64, execution *transferExecution, err error, failureKind string) bool {
	message := "file transfer failed"
	if execution != nil {
		message = execution.boundary.Redact(fileTransferFailureMessage(err))
	}
	return s.persistFileTransferTerminal(runtime, transferID, func(ctx context.Context) (bool, error) {
		return runtime.store.FailWithKind(ctx, transferID, message, failureKind)
	})
}

func (s Handlers) finishFileTransferError(runtime *Runtime, transferID int64, ctx context.Context, execution *transferExecution, err error) bool {
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		return s.failFileTransfer(runtime, transferID, execution, err, filetransfer.FailureKindOutcomeUnknown)
	}
	switch classifyFileTransferInterruption(ctx, err) {
	case fileTransferTimedOut:
		return s.failFileTransfer(runtime, transferID, execution, errFileTransferTimedOut, filetransfer.FailureKindTimeout)
	case fileTransferCanceledByUser:
		return s.cancelFileTransferRecord(runtime, transferID, "canceled by local user")
	default:
		return s.failFileTransfer(runtime, transferID, execution, err, filetransfer.FailureKindUnknown)
	}
}

func (s Handlers) finishFileTransferBatchError(runtime *Runtime, batchID int64, ctx context.Context, execution *transferExecution, err error) bool {
	message := "file transfer batch failed"
	if execution != nil {
		message = execution.boundary.Redact(fileTransferFailureMessage(err))
	}
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		return s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
			return runtime.store.FailBatchWithKind(ctx, batchID, message, filetransfer.FailureKindOutcomeUnknown)
		})
	}
	switch classifyFileTransferInterruption(ctx, err) {
	case fileTransferTimedOut:
		return s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
			return runtime.store.FailBatchWithKind(ctx, batchID, "file transfer batch timed out", filetransfer.FailureKindTimeout)
		})
	case fileTransferCanceledByUser:
		return s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
			return runtime.store.CancelBatch(ctx, batchID, "canceled by local user")
		})
	default:
		return s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
			return runtime.store.FailBatchWithKind(ctx, batchID, message, filetransfer.FailureKindUnknown)
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

func (s Handlers) cancelFileTransferRecord(runtime *Runtime, transferID int64, message string) bool {
	return s.persistFileTransferTerminal(runtime, transferID, func(ctx context.Context) (bool, error) {
		return runtime.store.Cancel(ctx, transferID, message)
	})
}

func (s Handlers) persistFileTransferTerminal(runtime *Runtime, transferID int64, persist func(context.Context) (bool, error)) bool {
	return transferjobs.PersistTerminal(runtime.finalization.Context(), "file transfer", transferID, persist, func(ctx context.Context) (string, error) {
		item, err := runtime.store.Get(ctx, transferID)
		return item.Status, err
	})
}

func (s Handlers) persistFileTransferBatchTerminal(runtime *Runtime, batchID int64, persist func(context.Context) (bool, error)) bool {
	return transferjobs.PersistTerminal(runtime.finalization.Context(), "file transfer batch", batchID, persist, func(ctx context.Context) (string, error) {
		item, err := runtime.store.GetBatch(ctx, batchID)
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
