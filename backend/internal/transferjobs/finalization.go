package transferjobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

const fileTransferFinalizationAttempts = 3
const fileTransferFinalizationAttemptTimeout = 2 * time.Second

const fileTransferOutcomeUnknownMessage = "the remote transfer may have completed, but AIPermission could not persist the final outcome; inspect the destination before retrying"

// FinalizationStore is the persistence port for one transfer outcome.
type FinalizationStore interface {
	Complete(context.Context, int64, int64, string) (bool, error)
	Get(context.Context, int64) (filetransfer.Record, error)
	SyncHistory(context.Context, int64) error
	FailWithKind(context.Context, int64, string, string) (bool, error)
}

// BatchFinalizationStore is the persistence port for one transfer batch.
type BatchFinalizationStore interface {
	CompleteBatch(context.Context, int64) (bool, error)
	GetBatch(context.Context, int64) (filetransfer.BatchRecord, error)
	FailBatchWithKind(context.Context, int64, string, string) (bool, error)
}

// FinalizeSuccessfulFileTransfer persists a successful remote effect without
// converting uncertain local persistence into a retry-safe failure.
func FinalizeSuccessfulFileTransfer(ctx context.Context, store FinalizationStore, transferID int64, transferred int64, checksum string) (bool, error) {
	var finalizationErrors []error
	canonicalCompleted := false

	for attempt := 0; attempt < fileTransferFinalizationAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, fileTransferFinalizationAttemptTimeout)
		changed, err := store.Complete(attemptCtx, transferID, transferred, checksum)
		if err == nil && changed {
			cancel()
			return true, nil
		}
		if err != nil {
			finalizationErrors = append(finalizationErrors, err)
		}

		current, readErr := store.Get(attemptCtx, transferID)
		if readErr != nil {
			finalizationErrors = append(finalizationErrors, readErr)
			cancel()
			continue
		}
		switch current.Status {
		case filetransfer.StatusCompleted:
			canonicalCompleted = true
			if syncErr := store.SyncHistory(attemptCtx, transferID); syncErr == nil {
				cancel()
				return true, nil
			} else {
				finalizationErrors = append(finalizationErrors, syncErr)
			}
		case filetransfer.StatusFailed, filetransfer.StatusCanceled:
			cancel()
			return false, errors.Join(finalizationErrors...)
		}
		cancel()
	}

	if canonicalCompleted {
		return true, fmt.Errorf("file transfer completed but history projection could not be synchronized: %w", errors.Join(finalizationErrors...))
	}

	if len(finalizationErrors) == 0 {
		finalizationErrors = append(finalizationErrors, errors.New("canonical transfer completion was not confirmed"))
	}
	failCtx, cancel := context.WithTimeout(ctx, fileTransferFinalizationAttemptTimeout)
	defer cancel()
	changed, failErr := store.FailWithKind(failCtx, transferID, fileTransferOutcomeUnknownMessage, filetransfer.FailureKindOutcomeUnknown)
	if failErr != nil {
		finalizationErrors = append(finalizationErrors, failErr)
	}
	if !changed {
		current, readErr := store.Get(failCtx, transferID)
		if readErr != nil {
			finalizationErrors = append(finalizationErrors, readErr)
		} else if current.Status == filetransfer.StatusCompleted {
			if syncErr := store.SyncHistory(failCtx, transferID); syncErr == nil {
				return true, nil
			} else {
				finalizationErrors = append(finalizationErrors, syncErr)
			}
			return true, fmt.Errorf("file transfer completed during finalization recovery: %w", errors.Join(finalizationErrors...))
		}
	}
	return false, fmt.Errorf("file transfer finalization outcome is unknown: %w", errors.Join(finalizationErrors...))
}

// FinalizeFileTransferBatch closes a batch after all item runners finish.
func FinalizeFileTransferBatch(ctx context.Context, store BatchFinalizationStore, batchID int64) error {
	var finalizationErrors []error
	for attempt := 0; attempt < fileTransferFinalizationAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, fileTransferFinalizationAttemptTimeout)
		changed, err := store.CompleteBatch(attemptCtx, batchID)
		if err == nil && changed {
			cancel()
			return nil
		}
		if err != nil {
			finalizationErrors = append(finalizationErrors, err)
		}
		current, readErr := store.GetBatch(attemptCtx, batchID)
		cancel()
		if readErr != nil {
			finalizationErrors = append(finalizationErrors, readErr)
			continue
		}
		if current.Status == filetransfer.StatusCompleted || current.Status == filetransfer.StatusFailed || current.Status == filetransfer.StatusCanceled {
			return nil
		}
	}
	if len(finalizationErrors) == 0 {
		finalizationErrors = append(finalizationErrors, errors.New("canonical transfer batch completion was not confirmed"))
	}
	failCtx, cancel := context.WithTimeout(ctx, fileTransferFinalizationAttemptTimeout)
	defer cancel()
	_, failErr := store.FailBatchWithKind(failCtx, batchID, "transfer items finished but the batch outcome could not be persisted", filetransfer.FailureKindLocalPersistence)
	if failErr != nil {
		finalizationErrors = append(finalizationErrors, failErr)
	}
	return fmt.Errorf("file transfer batch finalization failed: %w", errors.Join(finalizationErrors...))
}
