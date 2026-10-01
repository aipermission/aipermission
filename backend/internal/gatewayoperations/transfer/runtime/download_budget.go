package transferruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

type batchDownloadBudget struct {
	mu    sync.Mutex
	limit int64
	used  int64
}

type downloadAttemptBudget struct {
	batch    *batchDownloadBudget
	limit    int64
	observed int64
	err      error
}

func (budget *batchDownloadBudget) begin(objectLimit int64) (*downloadAttemptBudget, error) {
	if budget == nil {
		return nil, fmt.Errorf("%w: download batch budget is unavailable", connectors.ErrTransferByteLimit)
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	remaining := budget.limit - budget.used
	if objectLimit <= 0 || remaining <= 0 {
		return nil, fmt.Errorf("%w: download batch has no remaining byte budget", connectors.ErrTransferByteLimit)
	}
	return &downloadAttemptBudget{batch: budget, limit: min(objectLimit, remaining)}, nil
}

// Progress, result and disk size are high-water observations of the same bytes,
// not independent charges. Failed attempts never refund already written bytes.
func (attempt *downloadAttemptBudget) record(transferred int64) error {
	budget := attempt.batch
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if transferred < 0 {
		attempt.err = fmt.Errorf("%w: negative download byte measurement", connectors.ErrTransferByteLimit)
		return attempt.err
	}
	if transferred > attempt.observed {
		delta := transferred - attempt.observed
		remaining := budget.limit - budget.used
		budget.used += min(delta, remaining)
		attempt.observed = transferred
		if delta > remaining || transferred > attempt.limit {
			attempt.err = fmt.Errorf("%w: actual download exceeded its accepted byte budget", connectors.ErrTransferByteLimit)
		}
	}
	return attempt.err
}

func (attempt *downloadAttemptBudget) reconcile(localPath string, reported int64, successful bool) error {
	_ = attempt.record(reported)
	info, err := os.Lstat(localPath)
	if err == nil {
		if !info.Mode().IsRegular() {
			err = fmt.Errorf("local download staging is not a regular file")
		} else {
			_ = attempt.record(info.Size())
		}
	}
	if errors.Is(err, os.ErrNotExist) && !successful {
		err = nil
	}
	attempt.batch.mu.Lock()
	defer attempt.batch.mu.Unlock()
	if err != nil {
		attempt.err = errors.Join(attempt.err, fmt.Errorf("%w: cannot account local download staging: %w", connectors.ErrTransferByteLimit, err))
	}
	return attempt.err
}

func (attempt *downloadAttemptBudget) bytes() int64 {
	attempt.batch.mu.Lock()
	defer attempt.batch.mu.Unlock()
	return attempt.observed
}

func (s Runner) downloadBudget(runtime *Runtime, batch filetransfer.BatchRecord) (*batchDownloadBudget, error) {
	if batch.Direction != filetransfer.DirectionDownload {
		return nil, nil
	}
	if s.maxBatchBytes <= 0 || s.maxObjectBytes <= 0 {
		return nil, fmt.Errorf("%w: download byte limits are required", connectors.ErrTransferByteLimit)
	}
	budget := &batchDownloadBudget{limit: s.maxBatchBytes}
	for _, item := range batch.Items {
		attempt := &downloadAttemptBudget{batch: budget, limit: s.maxObjectBytes}
		if item.TempPath != "" {
			if !s.TempPathAllowed(runtime, item.TempPath) {
				return nil, fmt.Errorf("%w: unsafe download staging path", connectors.ErrTransferByteLimit)
			}
			if err := attempt.reconcile(item.TempPath, item.TransferredBytes, false); err != nil {
				return nil, err
			}
		} else if err := attempt.record(item.TransferredBytes); err != nil {
			return nil, err
		}
	}
	return budget, nil
}

func (s Runner) downloadBatchItem(ctx context.Context, runtime *Runtime, item filetransfer.Record, execution Execution, options connectors.TransferOptions, budget *batchDownloadBudget) (connectors.TransferResult, error) {
	if !s.TempPathAllowed(runtime, item.TempPath) {
		return connectors.TransferResult{}, fmt.Errorf("%w: unsafe download staging path", connectors.ErrTransferByteLimit)
	}
	attempt, err := budget.begin(s.maxObjectBytes)
	if err != nil {
		return connectors.TransferResult{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	progress := options.Progress
	options.MaxBytes = attempt.limit
	options.Progress = func(transferred, total int64) {
		if attempt.record(transferred) != nil {
			cancel()
		}
		if progress != nil {
			progress(transferred, total)
		}
	}
	result, downloadErr := execution.Adapter.DownloadFile(ctx, execution.Gateway, execution.Runtime, item.RuntimeID, item.RemotePath, item.TempPath, options)
	budgetErr := attempt.reconcile(item.TempPath, result.Bytes, downloadErr == nil)
	result.Bytes = attempt.bytes()
	return result, errors.Join(downloadErr, budgetErr)
}

func (s Runner) rejectDownloadBatchBudget(runtime *Runtime, batchID int64, execution *Execution, err error) {
	message := credentialSafeErrorMessage(execution, "download batch byte budget rejected", err)
	durable := s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
		return runtime.store.FailBatchWithKind(ctx, batchID, message, filetransfer.FailureKindValidation)
	})
	s.cleanupBatchTempsIfDurable(runtime, batchID, durable)
}
