package filetransferhttp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

const (
	fileTransferPersistenceRetryInterval  = 100 * time.Millisecond
	fileTransferPersistenceMaxRetryDelay  = 2 * time.Second
	fileTransferPersistenceAttemptTimeout = 2 * time.Second
)

var (
	errTransferRuntimeClosing = errors.New("file transfer runtime is shutting down")
	errTransferLaunchInvalid  = errors.New("file transfer launch is invalid")
)

func transferLaunchError(result transferjobs.LaunchResult) error {
	switch result {
	case transferjobs.LaunchAccepted, transferjobs.LaunchAlreadyRunning:
		return nil
	case transferjobs.LaunchRejectedClosed:
		return errTransferRuntimeClosing
	default:
		return errTransferLaunchInvalid
	}
}

func (s Handlers) rejectTransferLaunch(runtime *Runtime, transferID int64) {
	durable := s.persistFileTransferTerminal(runtime, transferID, func(ctx context.Context) (bool, error) {
		return runtime.store.FailWithKind(ctx, transferID, "file transfer could not start", filetransfer.FailureKindInterrupted)
	})
	if durable {
		s.removeTransferTemp(runtime, transferID)
	}
}

func (s Handlers) rejectBatchLaunch(runtime *Runtime, batchID int64) {
	durable := s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
		return runtime.store.FailBatchWithKind(ctx, batchID, "file transfer batch could not start", filetransfer.FailureKindInterrupted)
	})
	s.cleanupBatchTempsIfDurable(runtime, batchID, durable)
}

func (s Handlers) launchUpload(ctx context.Context, runtime *Runtime, transferID int64, overwrite bool, accepted *transferExecution) error {
	item, err := runtime.store.Get(ctx, transferID)
	if err != nil {
		return err
	}
	var execution transferExecution
	if accepted == nil {
		execution, err = s.resolveTransferExecution(ctx, runtime, item.RuntimeID)
		if err != nil {
			return err
		}
	} else {
		execution = *accepted
		if execution.runtimeID != item.RuntimeID {
			return connectortargets.ErrRuntimeSurfaceNotFound
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferTimeout)
	return transferLaunchError(runtime.jobs.Files.TryLaunch(transferID, cancel, func() {
		s.runUpload(ctx, runtime, transferID, overwrite, execution)
	}))
}

func (s Handlers) runUpload(ctx context.Context, runtime *Runtime, transferID int64, overwrite bool, execution transferExecution) {
	ok, err := runtime.store.MarkRunning(ctx, transferID)
	if err != nil {
		log.Printf("mark file upload running failed transfer=%d error=%v", transferID, err)
		return
	}
	if !ok {
		return
	}
	item, err := runtime.store.Get(ctx, transferID)
	if err != nil {
		if s.finishFileTransferError(runtime, transferID, ctx, &execution, err) {
			s.removeTransferTemp(runtime, transferID)
		}
		log.Printf("read file upload failed transfer=%d error=%v", transferID, err)
		return
	}
	result, err := execution.adapter.UploadFile(ctx, execution.gateway, execution.runtime, item.RuntimeID, item.TempPath, item.RemotePath, overwrite, connectorapi.TransferOptions{
		Progress: s.transferProgress(runtime, transferID),
	})
	if err != nil {
		if s.finishFileTransferError(runtime, transferID, ctx, &execution, err) {
			s.removeTransferTempPath(item.TempPath)
		}
		return
	}
	completed, err := transferjobs.FinalizeSuccessfulFileTransfer(runtime.finalization.Context(), runtime.store, transferID, result.Bytes, result.ChecksumSHA256)
	if err != nil {
		log.Printf("complete file upload failed transfer=%d error=%v", transferID, err)
	}
	if completed || s.transferTerminalDurable(runtime, transferID) {
		s.removeTransferTempPath(item.TempPath)
	}
	if !completed {
		return
	}
	s.writeObservationAudit(context.Background(), runtime, "user", nil, item.RuntimeID, "file_transfer.upload.completed", map[string]any{
		"transfer_id":     transferID,
		"remote_path":     item.RemotePath,
		"bytes":           result.Bytes,
		"checksum_sha256": result.ChecksumSHA256,
		"duration_ms":     result.DurationMS,
	})
}

func (s Handlers) launchDownload(ctx context.Context, runtime *Runtime, transferID int64, accepted *transferExecution) error {
	item, err := runtime.store.Get(ctx, transferID)
	if err != nil {
		return err
	}
	var execution transferExecution
	if accepted == nil {
		execution, err = s.resolveTransferExecution(ctx, runtime, item.RuntimeID)
		if err != nil {
			return err
		}
	} else {
		execution = *accepted
		if execution.runtimeID != item.RuntimeID {
			return connectortargets.ErrRuntimeSurfaceNotFound
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferTimeout)
	return transferLaunchError(runtime.jobs.Files.TryLaunch(transferID, cancel, func() {
		s.runDownload(ctx, runtime, transferID, execution)
	}))
}

func (s Handlers) runDownload(ctx context.Context, runtime *Runtime, transferID int64, execution transferExecution) {
	ok, err := runtime.store.MarkRunning(ctx, transferID)
	if err != nil {
		log.Printf("mark file download running failed transfer=%d error=%v", transferID, err)
		return
	}
	if !ok {
		return
	}
	item, err := runtime.store.Get(ctx, transferID)
	if err != nil {
		if s.finishFileTransferError(runtime, transferID, ctx, &execution, err) {
			s.removeTransferTemp(runtime, transferID)
		}
		log.Printf("read file download failed transfer=%d error=%v", transferID, err)
		return
	}
	result, err := execution.adapter.DownloadFile(ctx, execution.gateway, execution.runtime, item.RuntimeID, item.RemotePath, item.TempPath, connectorapi.TransferOptions{
		Progress: s.transferProgress(runtime, transferID),
		MaxBytes: maxFileTransferObjectBytes,
	})
	if err != nil {
		if s.finishFileTransferError(runtime, transferID, ctx, &execution, err) {
			s.removeTransferTempPath(item.TempPath)
		}
		return
	}
	completed, err := transferjobs.FinalizeSuccessfulFileTransfer(runtime.finalization.Context(), runtime.store, transferID, result.Bytes, result.ChecksumSHA256)
	if err != nil {
		log.Printf("complete file download failed transfer=%d error=%v", transferID, err)
	}
	if completed || s.transferTerminalDurable(runtime, transferID) {
		s.scheduleTransferTempCleanup(item.TempPath)
	}
	if !completed {
		return
	}
	s.writeObservationAudit(context.Background(), runtime, "user", nil, item.RuntimeID, "file_transfer.download.completed", map[string]any{
		"transfer_id":     transferID,
		"remote_path":     item.RemotePath,
		"bytes":           result.Bytes,
		"checksum_sha256": result.ChecksumSHA256,
		"duration_ms":     result.DurationMS,
	})
}

func (s Handlers) launchTransferBatch(ctx context.Context, runtime *Runtime, batchID int64, overwrite bool, accepted *transferExecution) error {
	batch, err := runtime.store.GetBatch(ctx, batchID)
	if err != nil {
		return err
	}
	var execution transferExecution
	if accepted == nil {
		execution, err = s.resolveTransferExecution(ctx, runtime, batch.RuntimeID)
		if err != nil {
			return err
		}
	} else {
		execution = *accepted
		if execution.runtimeID != batch.RuntimeID {
			return connectortargets.ErrRuntimeSurfaceNotFound
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferBatchTimeout)
	return transferLaunchError(runtime.jobs.Batches.TryLaunch(batchID, cancel, func() {
		s.runTransferBatch(ctx, runtime, batchID, overwrite, execution)
	}))
}

func (s Handlers) runTransferBatch(ctx context.Context, runtime *Runtime, batchID int64, overwrite bool, execution transferExecution) {
	control := &transferjobs.Control{}
	runtime.jobs.Batches.RegisterControl(batchID, control)
	defer runtime.jobs.Batches.UnregisterControl(batchID)

	if ok, err := runtime.store.MarkBatchRunning(ctx, batchID); err != nil {
		log.Printf("claim file transfer batch failed batch=%d error=%v", batchID, err)
		return
	} else if !ok {
		return
	}
	batch, err := runtime.store.GetBatch(ctx, batchID)
	if err != nil {
		s.cleanupBatchTempsIfDurable(runtime, batchID, s.finishFileTransferBatchError(runtime, batchID, ctx, &execution, err))
		log.Printf("read file transfer batch before run failed batch=%d error=%v", batchID, err)
		return
	}
	if batch.Direction == filetransfer.DirectionDownload {
		if err := s.validateDownloadBatchBeforeRun(ctx, runtime, batch, execution); err != nil {
			if classifyFileTransferInterruption(ctx, err) != fileTransferNotInterrupted {
				s.cleanupBatchTempsIfDurable(runtime, batchID, s.finishFileTransferBatchError(runtime, batchID, ctx, &execution, err))
				return
			}
			message := credentialSafeFileTransferErrorMessage(&execution, "file transfer batch guardrail rejected", err)
			log.Printf("reject file transfer batch before run batch=%d error=%s", batchID, message)
			durable := s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
				return runtime.store.FailBatchWithKind(ctx, batchID, message, filetransfer.FailureKindValidation)
			})
			s.cleanupBatchTempsIfDurable(runtime, batchID, durable)
			s.writeObservationAudit(context.Background(), runtime, "gateway", nil, batch.RuntimeID, "file_transfer.batch.guardrail_rejected", map[string]any{
				"batch_id": batchID,
				"error":    message,
			})
			return
		}
	}
	batch, err = runtime.store.GetBatch(ctx, batchID)
	if err != nil {
		s.cleanupBatchTempsIfDurable(runtime, batchID, s.finishFileTransferBatchError(runtime, batchID, ctx, &execution, err))
		log.Printf("read file transfer batch failed batch=%d error=%v", batchID, err)
		return
	}
	for {
		if err := control.Wait(ctx); err != nil {
			break
		}
		latest, err := runtime.store.NextBatchPendingItem(ctx, batchID)
		if errors.Is(err, filetransfer.ErrNotFound) {
			break
		}
		if err != nil {
			log.Printf("read next file transfer batch item failed batch=%d error=%v", batchID, err)
			s.cleanupBatchTempsIfDurable(runtime, batchID, s.finishFileTransferBatchError(runtime, batchID, ctx, &execution, err))
			return
		}
		s.runTransferBatchItem(ctx, runtime, latest.ID, overwrite, control, execution)
		_ = runtime.store.RecalculateBatch(context.Background(), batchID)
		if ctx.Err() != nil {
			break
		}
	}
	if ctx.Err() != nil {
		s.cleanupBatchTempsIfDurable(runtime, batchID, s.finishFileTransferBatchError(runtime, batchID, ctx, &execution, ctx.Err()))
		return
	}
	batch, err = transferjobs.PrepareFileTransferBatch(runtime.finalization.Context(), runtime.store, batchID)
	if err != nil {
		log.Printf("prepare completed file transfer batch failed batch=%d error=%v", batchID, err)
		s.cleanupBatchTempsIfDurable(runtime, batchID, s.finishFileTransferBatchError(runtime, batchID, ctx, &execution, err))
		return
	}
	if batch.Direction == filetransfer.DirectionDownload && batch.FailedItems == 0 && batch.CompletedItems > 0 {
		if len(batch.Items) > 1 {
			archivePath, err := s.createDownloadArchive(batch)
			if err != nil {
				log.Printf("create file transfer archive failed batch=%d error=%v", batchID, err)
				message := credentialSafeFileTransferErrorMessage(&execution, "create file transfer archive failed", err)
				durable := s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
					return runtime.store.FailBatchWithKind(ctx, batchID, message, filetransfer.FailureKindUnknown)
				})
				s.cleanupBatchTempsIfDurable(runtime, batchID, durable)
				return
			}
			if !s.persistDownloadBatchArchive(runtime.finalization.Context(), runtime, batch, archivePath, &execution) {
				return
			}
			batch.ArchivePath = archivePath
		}
	}
	if err := transferjobs.FinalizeFileTransferBatch(runtime.finalization.Context(), runtime.store, batchID); err != nil {
		log.Printf("complete file transfer batch failed batch=%d error=%v", batchID, err)
		return
	}
	if batch.Direction == filetransfer.DirectionDownload && batch.FailedItems == 0 && batch.CompletedItems > 0 {
		s.scheduleBatchTempCleanup(batch)
	}
}

func (s Handlers) persistDownloadBatchArchive(ctx context.Context, runtime *Runtime, batch filetransfer.BatchRecord, archivePath string, execution *transferExecution) bool {
	attemptCtx, cancel := context.WithTimeout(ctx, fileTransferPersistenceAttemptTimeout)
	err := runtime.store.SetBatchArchive(attemptCtx, batch.ID, archivePath)
	cancel()
	if err != nil {
		log.Printf("set file transfer archive failed batch=%d error=%v", batch.ID, err)
		message := credentialSafeFileTransferErrorMessage(execution, "persist file transfer archive failed", err)
		if s.persistDownloadBatchArchiveFailure(ctx, runtime, batch.ID, message) {
			batch.ArchivePath = archivePath
			s.scheduleBatchTempCleanup(batch)
		}
		return false
	}
	return true
}

func (s Handlers) persistDownloadBatchArchiveFailure(ctx context.Context, runtime *Runtime, batchID int64, message string) bool {
	loggedFailure := false
	retryDelay := fileTransferPersistenceRetryInterval
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, fileTransferPersistenceAttemptTimeout)
		failed, err := runtime.store.FailBatchWithKind(attemptCtx, batchID, message, filetransfer.FailureKindLocalPersistence)
		if err == nil {
			if failed {
				cancel()
				return true
			}
			current, readErr := runtime.store.GetBatch(attemptCtx, batchID)
			if readErr == nil && fileTransferBatchTerminal(current.Status) {
				cancel()
				return true
			}
			err = readErr
			if err == nil {
				err = fmt.Errorf("batch remained %s", current.Status)
			}
		}
		cancel()
		if !loggedFailure {
			log.Printf("persist file transfer archive failure state delayed batch=%d error=%v", batchID, err)
			loggedFailure = true
		}
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			checkCtx, checkCancel := context.WithTimeout(context.Background(), fileTransferPersistenceAttemptTimeout)
			current, readErr := runtime.store.GetBatch(checkCtx, batchID)
			checkCancel()
			if readErr == nil && fileTransferBatchTerminal(current.Status) {
				return true
			}
			log.Printf("persist file transfer archive failure state stopped batch=%d error=%v", batchID, ctx.Err())
			return false
		case <-timer.C:
		}
		retryDelay = min(retryDelay*2, fileTransferPersistenceMaxRetryDelay)
	}
}

func fileTransferBatchTerminal(status string) bool {
	return status == filetransfer.StatusCompleted || status == filetransfer.StatusFailed || status == filetransfer.StatusCanceled
}

func (s Handlers) validateDownloadBatchBeforeRun(ctx context.Context, runtime *Runtime, batch filetransfer.BatchRecord, execution transferExecution) error {
	sizes := make(map[int64]int64, len(batch.Items))
	var totalSize int64
	for _, item := range batch.Items {
		if item.Status != filetransfer.StatusPending {
			continue
		}
		status, err := execution.adapter.StatRemotePath(ctx, execution.gateway, execution.runtime, batch.RuntimeID, item.RemotePath)
		if err != nil {
			return fmt.Errorf("stat %s before download: %w", item.RemotePath, err)
		}
		if !status.Exists || status.Type != "file" {
			return fmt.Errorf("remote path %s is not an existing regular file", item.RemotePath)
		}
		if err := validateDownloadObjectSize(status.Size); err != nil {
			return fmt.Errorf("%s: %w", item.RemotePath, err)
		}
		if totalSize > maxFileTransferBatchBytes-status.Size {
			return fmt.Errorf("download batch cannot exceed %s total size", formatFileTransferLimit(maxFileTransferBatchBytes))
		}
		totalSize += status.Size
		sizes[item.ID] = status.Size
	}
	if len(sizes) == 0 {
		return fmt.Errorf("download batch has no pending items")
	}
	return runtime.store.UpdatePendingBatchItemSizes(ctx, batch.ID, sizes)
}

func (s Handlers) runTransferBatchItem(ctx context.Context, runtime *Runtime, transferID int64, overwrite bool, control *transferjobs.Control, execution transferExecution) {
	itemCtx, itemCancel := context.WithCancel(ctx)
	runtime.jobs.Files.RegisterCancel(transferID, itemCancel)
	runtime.jobs.Files.RegisterControl(transferID, control)
	defer runtime.jobs.Files.UnregisterCancel(transferID)
	defer runtime.jobs.Files.UnregisterControl(transferID)
	defer itemCancel()

	ok, err := runtime.store.MarkRunning(itemCtx, transferID)
	if err != nil {
		if s.finishFileTransferError(runtime, transferID, itemCtx, &execution, err) {
			s.removeTransferTemp(runtime, transferID)
		}
		log.Printf("mark file transfer running failed transfer=%d error=%v", transferID, err)
		return
	}
	if !ok {
		return
	}
	item, err := runtime.store.Get(itemCtx, transferID)
	if err != nil {
		if s.finishFileTransferError(runtime, transferID, itemCtx, &execution, err) {
			s.removeTransferTemp(runtime, transferID)
		}
		log.Printf("read file transfer failed transfer=%d error=%v", transferID, err)
		return
	}
	options := connectorapi.TransferOptions{
		Progress: s.transferProgress(runtime, transferID),
		Wait:     control.Wait,
		MaxBytes: maxFileTransferObjectBytes,
	}
	var result connectorapi.TransferResult
	if item.Direction == filetransfer.DirectionUpload {
		result, err = execution.adapter.UploadFile(itemCtx, execution.gateway, execution.runtime, item.RuntimeID, item.TempPath, item.RemotePath, overwrite, options)
	} else {
		result, err = execution.adapter.DownloadFile(itemCtx, execution.gateway, execution.runtime, item.RuntimeID, item.RemotePath, item.TempPath, options)
	}
	if err != nil {
		if s.finishFileTransferError(runtime, transferID, itemCtx, &execution, err) {
			s.removeTransferTempPath(item.TempPath)
		}
		return
	}
	completed, err := transferjobs.FinalizeSuccessfulFileTransfer(runtime.finalization.Context(), runtime.store, transferID, result.Bytes, result.ChecksumSHA256)
	if err != nil {
		log.Printf("complete file transfer failed transfer=%d error=%v", transferID, err)
	}
	if item.Direction == filetransfer.DirectionUpload && (completed || s.transferTerminalDurable(runtime, transferID)) {
		s.removeTransferTempPath(item.TempPath)
	}
	if !completed {
		return
	}
	if item.Direction == filetransfer.DirectionDownload && item.BatchID == 0 {
		s.scheduleTransferTempCleanup(item.TempPath)
	}
}

func (s Handlers) transferProgress(runtime *Runtime, transferID int64) connectorapi.TransferProgress {
	var lastWrite time.Time
	started := time.Now()
	return func(transferred int64, total int64) {
		now := time.Now()
		if transferred != total && now.Sub(lastWrite) < 250*time.Millisecond {
			return
		}
		lastWrite = now
		bytesPerSecond, etaSeconds := transferSpeedAndETA(transferred, total, now.Sub(started))
		if err := runtime.store.UpdateProgressStats(context.Background(), transferID, transferred, total, bytesPerSecond, etaSeconds); err != nil {
			log.Printf("update file transfer progress failed transfer=%d error=%v", transferID, err)
		}
		item, err := runtime.store.Get(context.Background(), transferID)
		if err == nil && item.BatchID > 0 {
			if err := runtime.store.RecalculateBatch(context.Background(), item.BatchID); err != nil {
				log.Printf("recalculate file transfer batch progress failed batch=%d error=%v", item.BatchID, err)
			}
		}
	}
}
