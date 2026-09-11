package transferruntime

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

var errFileTransferTimedOut = errors.New("file transfer timed out")

func (s Runner) failFileTransfer(runtime *Runtime, transferID int64, execution *Execution, err error, failureKind string) bool {
	message := "file transfer failed"
	if execution != nil {
		message = execution.Boundary.Redact(fileTransferFailureMessage(err))
	}
	return s.persistFileTransferTerminal(runtime, transferID, func(ctx context.Context) (bool, error) {
		return runtime.store.FailWithKind(ctx, transferID, message, failureKind)
	})
}

func (s Runner) finishFileTransferError(runtime *Runtime, transferID int64, ctx context.Context, execution *Execution, err error) bool {
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

func (s Runner) finishFileTransferBatchError(runtime *Runtime, batchID int64, ctx context.Context, execution *Execution, err error) bool {
	message := "file transfer batch failed"
	if execution != nil {
		message = execution.Boundary.Redact(fileTransferFailureMessage(err))
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

func (s Runner) cancelFileTransferRecord(runtime *Runtime, transferID int64, message string) bool {
	return s.persistFileTransferTerminal(runtime, transferID, func(ctx context.Context) (bool, error) {
		return runtime.store.Cancel(ctx, transferID, message)
	})
}

func (s Runner) persistFileTransferTerminal(runtime *Runtime, transferID int64, persist func(context.Context) (bool, error)) bool {
	return transferjobs.PersistTerminal(runtime.finalization.Context(), "file transfer", transferID, persist, func(ctx context.Context) (string, error) {
		item, err := runtime.store.Get(ctx, transferID)
		return item.Status, err
	})
}

func (s Runner) persistFileTransferBatchTerminal(runtime *Runtime, batchID int64, persist func(context.Context) (bool, error)) bool {
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

func credentialSafeErrorMessage(execution *Execution, prefix string, err error) string {
	if err == nil || execution == nil {
		return prefix
	}
	if execution.Boundary.Redact(err.Error()) != err.Error() {
		return prefix
	}
	message := connectorapi.PresentedErrorMessage(execution.Adapter, prefix, err)
	if execution.Boundary.Redact(message) != message {
		return prefix
	}
	return message
}
