package transferjobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

const fileTransferFinalizationAttempts = 3
const fileTransferFinalizationAttemptTimeout = 2 * time.Second
const fileTransferFinalizationRetryInterval = 100 * time.Millisecond
const fileTransferFinalizationMaxRetryDelay = 2 * time.Second

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

type BatchPreparationStore interface {
	RecalculateBatch(context.Context, int64) error
	GetBatch(context.Context, int64) (filetransfer.BatchRecord, error)
}

// PrepareFileTransferBatch refreshes and reads the aggregate after all item
// runners finish, retrying local persistence for the workspace lifetime.
func PrepareFileTransferBatch(ctx context.Context, store BatchPreparationStore, batchID int64) (filetransfer.BatchRecord, error) {
	retryDelay := fileTransferFinalizationRetryInterval
	lastErr := errors.New("file transfer batch aggregate was not prepared")
	for {
		if err := ctx.Err(); err != nil {
			return filetransfer.BatchRecord{}, fmt.Errorf("prepare file transfer batch: %w", errors.Join(err, lastErr))
		}
		attemptCtx, cancel := context.WithTimeout(ctx, fileTransferFinalizationAttemptTimeout)
		if err := store.RecalculateBatch(attemptCtx, batchID); err != nil {
			lastErr = err
		} else if batch, err := store.GetBatch(attemptCtx, batchID); err == nil {
			cancel()
			return batch, nil
		} else {
			lastErr = err
		}
		cancel()
		if err := waitForFinalizationRetry(ctx, retryDelay); err != nil {
			return filetransfer.BatchRecord{}, fmt.Errorf("prepare file transfer batch: %w", errors.Join(err, lastErr))
		}
		retryDelay = min(retryDelay*2, fileTransferFinalizationMaxRetryDelay)
	}
}

// PersistTerminal retries one local terminal transition until it is durable or
// the owning workspace lifetime ends.
func PersistTerminal(
	ctx context.Context,
	label string,
	id int64,
	persist func(context.Context) (bool, error),
	readStatus func(context.Context) (string, error),
) bool {
	retryDelay := fileTransferFinalizationRetryInterval
	loggedDelay := false
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, fileTransferFinalizationAttemptTimeout)
		changed, err := persist(attemptCtx)
		if err == nil && !changed {
			var status string
			status, err = readStatus(attemptCtx)
			if err == nil && fileTransferStatusTerminal(status) {
				cancel()
				return true
			}
			if err == nil {
				err = fmt.Errorf("%s remained %s", label, status)
			}
		}
		cancel()
		if err == nil && changed {
			return true
		}
		if !loggedDelay {
			log.Printf("persist %s terminal state delayed id=%d error=%v", label, id, err)
			loggedDelay = true
		}
		if err := waitForFinalizationRetry(ctx, retryDelay); err != nil {
			log.Printf("persist %s terminal state stopped id=%d error=%v", label, id, err)
			return false
		}
		retryDelay = min(retryDelay*2, fileTransferFinalizationMaxRetryDelay)
	}
}

// FinalizeSuccessfulFileTransfer persists a successful remote effect without
// converting uncertain local persistence into a retry-safe failure.
func FinalizeSuccessfulFileTransfer(ctx context.Context, store FinalizationStore, transferID int64, transferred int64, checksum string) (bool, error) {
	retryDelay := fileTransferFinalizationRetryInterval
	lastErr := errors.New("canonical transfer completion was not confirmed")
	canonicalCompleted := false
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return canonicalCompleted, fmt.Errorf("file transfer finalization stopped before a terminal state was durable: %w", errors.Join(err, lastErr))
		}
		attemptCtx, cancel := context.WithTimeout(ctx, fileTransferFinalizationAttemptTimeout)
		changed, err := store.Complete(attemptCtx, transferID, transferred, checksum)
		if err == nil && changed {
			cancel()
			return true, nil
		}
		if err != nil {
			lastErr = err
		}

		current, readErr := store.Get(attemptCtx, transferID)
		if readErr != nil {
			lastErr = readErr
		} else {
			switch current.Status {
			case filetransfer.StatusCompleted:
				canonicalCompleted = true
				if syncErr := store.SyncHistory(attemptCtx, transferID); syncErr == nil {
					cancel()
					return true, nil
				} else {
					lastErr = syncErr
				}
			case filetransfer.StatusFailed, filetransfer.StatusCanceled:
				cancel()
				return false, nil
			}
		}
		cancel()

		if attempt%fileTransferFinalizationAttempts == 0 {
			failCtx, failCancel := context.WithTimeout(ctx, fileTransferFinalizationAttemptTimeout)
			failed, failErr := store.FailWithKind(failCtx, transferID, fileTransferOutcomeUnknownMessage, filetransfer.FailureKindOutcomeUnknown)
			if failErr != nil {
				lastErr = failErr
			}
			if failed {
				failCancel()
				return false, fmt.Errorf("file transfer remote effect completed but its durable outcome is unknown: %w", lastErr)
			}
			current, readErr := store.Get(failCtx, transferID)
			if readErr == nil && current.Status == filetransfer.StatusCompleted {
				if syncErr := store.SyncHistory(failCtx, transferID); syncErr == nil {
					failCancel()
					return true, nil
				} else {
					lastErr = syncErr
				}
			} else if readErr != nil {
				lastErr = readErr
			} else if current.Status == filetransfer.StatusFailed || current.Status == filetransfer.StatusCanceled {
				failCancel()
				return false, nil
			}
			failCancel()
		}
		if err := waitForFinalizationRetry(ctx, retryDelay); err != nil {
			return canonicalCompleted, fmt.Errorf("file transfer finalization stopped before a terminal state was durable: %w", errors.Join(err, lastErr))
		}
		retryDelay = min(retryDelay*2, fileTransferFinalizationMaxRetryDelay)
	}
}

// FinalizeFileTransferBatch closes a batch after all item runners finish.
func FinalizeFileTransferBatch(ctx context.Context, store BatchFinalizationStore, batchID int64) error {
	retryDelay := fileTransferFinalizationRetryInterval
	lastErr := errors.New("canonical transfer batch completion was not confirmed")
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("file transfer batch finalization stopped before a terminal state was durable: %w", errors.Join(err, lastErr))
		}
		attemptCtx, cancel := context.WithTimeout(ctx, fileTransferFinalizationAttemptTimeout)
		changed, err := store.CompleteBatch(attemptCtx, batchID)
		if err == nil && changed {
			cancel()
			return nil
		}
		if err != nil {
			lastErr = err
		}
		current, readErr := store.GetBatch(attemptCtx, batchID)
		cancel()
		if readErr != nil {
			lastErr = readErr
		} else if current.Status == filetransfer.StatusCompleted || current.Status == filetransfer.StatusFailed || current.Status == filetransfer.StatusCanceled {
			return nil
		}
		if attempt%fileTransferFinalizationAttempts == 0 {
			failCtx, failCancel := context.WithTimeout(ctx, fileTransferFinalizationAttemptTimeout)
			failed, failErr := store.FailBatchWithKind(failCtx, batchID, "transfer items finished but the batch outcome could not be persisted", filetransfer.FailureKindLocalPersistence)
			if failErr != nil {
				lastErr = failErr
			}
			if failed {
				failCancel()
				return fmt.Errorf("file transfer batch completion could not be persisted and was recorded as failed: %w", lastErr)
			}
			current, readErr := store.GetBatch(failCtx, batchID)
			failCancel()
			if readErr != nil {
				lastErr = readErr
			} else if fileTransferStatusTerminal(current.Status) {
				return nil
			}
		}
		if err := waitForFinalizationRetry(ctx, retryDelay); err != nil {
			return fmt.Errorf("file transfer batch finalization stopped before a terminal state was durable: %w", errors.Join(err, lastErr))
		}
		retryDelay = min(retryDelay*2, fileTransferFinalizationMaxRetryDelay)
	}
}

func waitForFinalizationRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func fileTransferStatusTerminal(status string) bool {
	return status == filetransfer.StatusCompleted || status == filetransfer.StatusFailed || status == filetransfer.StatusCanceled
}
