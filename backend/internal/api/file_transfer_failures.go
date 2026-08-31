package api

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

var errFileTransferTimedOut = errors.New("file transfer timed out")

func (s fileTransferHandlers) failFileTransfer(runtime *databaseRuntime, transferID int64, err error, failureKind string) {
	message := fileTransferFailureMessage(err)
	changed, writeErr := runtime.fileTransfers.FailWithKind(context.Background(), transferID, message, failureKind)
	if writeErr != nil {
		log.Printf("fail file transfer failed transfer=%d error=%v", transferID, writeErr)
	}
	if !changed {
		return
	}
}

func (s fileTransferHandlers) finishFileTransferError(runtime *databaseRuntime, transferID int64, ctx context.Context, err error) {
	switch classifyFileTransferInterruption(ctx, err) {
	case fileTransferTimedOut:
		s.failFileTransfer(runtime, transferID, errFileTransferTimedOut, filetransfer.FailureKindTimeout)
	case fileTransferCanceledByUser:
		s.cancelFileTransferRecord(runtime, transferID, "canceled by local user")
	default:
		s.failFileTransfer(runtime, transferID, err, filetransfer.FailureKindUnknown)
	}
}

func (s fileTransferHandlers) finishFileTransferBatchError(runtime *databaseRuntime, batchID int64, ctx context.Context, err error) {
	switch classifyFileTransferInterruption(ctx, err) {
	case fileTransferTimedOut:
		if _, writeErr := runtime.fileTransfers.FailBatchWithKind(context.Background(), batchID, "file transfer batch timed out", filetransfer.FailureKindTimeout); writeErr != nil {
			log.Printf("mark file transfer batch timed out failed batch=%d error=%v", batchID, writeErr)
		}
	case fileTransferCanceledByUser:
		if _, writeErr := runtime.fileTransfers.CancelBatch(context.Background(), batchID, "canceled by local user"); writeErr != nil {
			log.Printf("cancel file transfer batch failed batch=%d error=%v", batchID, writeErr)
		}
	default:
		if _, writeErr := runtime.fileTransfers.FailBatchWithKind(context.Background(), batchID, fileTransferFailureMessage(err), filetransfer.FailureKindUnknown); writeErr != nil {
			log.Printf("fail file transfer batch failed batch=%d error=%v", batchID, writeErr)
		}
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
	changed, err := runtime.fileTransfers.Cancel(context.Background(), transferID, message)
	if err != nil {
		log.Printf("cancel file transfer failed transfer=%d error=%v", transferID, err)
		return
	}
	if !changed {
		return
	}
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
