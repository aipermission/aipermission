package transferjobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

var ErrEvidenceNotDurable = errors.New("file transfer final evidence is not durable")

type EvidenceStore interface {
	UpdateEvidence(context.Context, int64, int64, string) error
	Get(context.Context, int64) (filetransfer.Record, error)
}

// Final evidence precedes terminal cleanup. Retrying is monotonic and also
// records bytes from a runner finishing after its cancellation became durable.
func PersistFileTransferEvidence(ctx context.Context, store EvidenceStore, id, transferred int64, checksum string) error {
	if store == nil || id < 1 || transferred < 0 {
		return fmt.Errorf("%w: %w", ErrEvidenceNotDurable, filetransfer.ErrInvalidArgument)
	}
	retryDelay := fileTransferFinalizationRetryInterval
	lastErr := ErrEvidenceNotDurable
	loggedDelay := false
	checksum = strings.TrimSpace(checksum)
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %w", ErrEvidenceNotDurable, errors.Join(err, lastErr))
		}
		attemptCtx, cancel := context.WithTimeout(ctx, fileTransferFinalizationAttemptTimeout)
		err := store.UpdateEvidence(attemptCtx, id, transferred, checksum)
		if err == nil {
			current, readErr := store.Get(attemptCtx, id)
			err = readErr
			if err == nil && current.TransferredBytes >= transferred && (checksum == "" || current.ChecksumSHA256 == checksum) {
				cancel()
				return nil
			}
			if err == nil {
				err = ErrEvidenceNotDurable
			}
		}
		cancel()
		if !loggedDelay {
			log.Printf("persist file transfer evidence delayed id=%d error=%v", id, err)
			loggedDelay = true
		}
		lastErr = err
		if err := waitForFinalizationRetry(ctx, retryDelay); err != nil {
			return fmt.Errorf("%w: %w", ErrEvidenceNotDurable, errors.Join(err, lastErr))
		}
		retryDelay = min(retryDelay*2, fileTransferFinalizationMaxRetryDelay)
	}
}
