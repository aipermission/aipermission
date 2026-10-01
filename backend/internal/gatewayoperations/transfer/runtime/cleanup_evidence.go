package transferruntime

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func transferOwnerActive(runtime *Runtime, item filetransfer.Record) bool {
	return runtime.jobs != nil && (runtime.jobs.Files.Active(item.ID) || (item.BatchID > 0 && runtime.jobs.Batches.Active(item.BatchID)))
}

// An external cancellation can become durable before its worker exits. After
// restart, retained staging is the last source of actual download-byte evidence.
func (s Runner) prepareExpiredTransferCleanup(ctx context.Context, runtime *Runtime, transferID int64, value string) (bool, error) {
	item, err := runtime.store.Get(ctx, transferID)
	if err != nil {
		return false, err
	}
	if item.TempPath != value || !fileTransferTerminal(item.Status) || transferOwnerActive(runtime, item) {
		return false, nil
	}
	if item.Direction != filetransfer.DirectionDownload {
		return true, nil
	}
	info, err := os.Lstat(value)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect retained download evidence: %w", err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("retained download evidence is not a regular file")
	}
	if err := runtime.store.UpdateEvidence(ctx, transferID, info.Size(), ""); err != nil {
		return false, err
	}
	return true, nil
}
