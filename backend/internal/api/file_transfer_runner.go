package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

const (
	fileTransferPersistenceRetryInterval  = 100 * time.Millisecond
	fileTransferPersistenceMaxRetryDelay  = 2 * time.Second
	fileTransferPersistenceAttemptTimeout = 2 * time.Second
)

func (s fileTransferHandlers) launchUpload(runtime *databaseRuntime, transferID int64, overwrite bool) {
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferTimeout)
	runtime.transferJobs.Files.Launch(transferID, cancel, func() {
		s.runUpload(ctx, runtime, transferID, overwrite)
	})
}

func (s fileTransferHandlers) runUpload(ctx context.Context, runtime *databaseRuntime, transferID int64, overwrite bool) {
	ok, err := runtime.fileTransfers.MarkRunning(ctx, transferID)
	if err != nil {
		log.Printf("mark file upload running failed transfer=%d error=%v", transferID, err)
		return
	}
	if !ok {
		return
	}
	defer s.removeTransferTemp(runtime, transferID)
	item, err := runtime.fileTransfers.Get(ctx, transferID)
	if err != nil {
		s.finishFileTransferError(runtime, transferID, ctx, err)
		log.Printf("read file upload failed transfer=%d error=%v", transferID, err)
		return
	}
	adapter, err := s.fileTransferAdapter(ctx, runtime, item.RuntimeID)
	if err != nil {
		s.finishFileTransferError(runtime, transferID, ctx, err)
		return
	}
	ports := connectorFileTransferPortsForID(ctx, s.Server, runtime, item.RuntimeID)
	result, err := adapter.UploadFile(ctx, ports.gateway, ports.runtime, item.RuntimeID, item.TempPath, item.RemotePath, overwrite, connectorapi.TransferOptions{
		Progress: s.transferProgress(runtime, transferID),
	})
	if err != nil {
		s.finishFileTransferError(runtime, transferID, ctx, err)
		return
	}
	completed, err := transferjobs.FinalizeSuccessfulFileTransfer(runtime.finalization.Context(), runtime.fileTransfers, transferID, result.Bytes, result.ChecksumSHA256)
	if err != nil {
		log.Printf("complete file upload failed transfer=%d error=%v", transferID, err)
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

func (s fileTransferHandlers) launchDownload(runtime *databaseRuntime, transferID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferTimeout)
	runtime.transferJobs.Files.Launch(transferID, cancel, func() {
		s.runDownload(ctx, runtime, transferID)
	})
}

func (s fileTransferHandlers) runDownload(ctx context.Context, runtime *databaseRuntime, transferID int64) {
	ok, err := runtime.fileTransfers.MarkRunning(ctx, transferID)
	if err != nil {
		log.Printf("mark file download running failed transfer=%d error=%v", transferID, err)
		return
	}
	if !ok {
		return
	}
	item, err := runtime.fileTransfers.Get(ctx, transferID)
	if err != nil {
		s.finishFileTransferError(runtime, transferID, ctx, err)
		log.Printf("read file download failed transfer=%d error=%v", transferID, err)
		return
	}
	adapter, err := s.fileTransferAdapter(ctx, runtime, item.RuntimeID)
	if err != nil {
		s.finishFileTransferError(runtime, transferID, ctx, err)
		return
	}
	ports := connectorFileTransferPortsForID(ctx, s.Server, runtime, item.RuntimeID)
	result, err := adapter.DownloadFile(ctx, ports.gateway, ports.runtime, item.RuntimeID, item.RemotePath, item.TempPath, connectorapi.TransferOptions{
		Progress: s.transferProgress(runtime, transferID),
		MaxBytes: maxFileTransferObjectBytes,
	})
	if err != nil {
		_ = os.Remove(item.TempPath)
		s.finishFileTransferError(runtime, transferID, ctx, err)
		return
	}
	completed, err := transferjobs.FinalizeSuccessfulFileTransfer(runtime.finalization.Context(), runtime.fileTransfers, transferID, result.Bytes, result.ChecksumSHA256)
	if err != nil {
		log.Printf("complete file download failed transfer=%d error=%v", transferID, err)
	}
	if !completed {
		s.scheduleTransferTempCleanup(item.TempPath)
		return
	}
	s.scheduleTransferTempCleanup(item.TempPath)
	s.writeObservationAudit(context.Background(), runtime, "user", nil, item.RuntimeID, "file_transfer.download.completed", map[string]any{
		"transfer_id":     transferID,
		"remote_path":     item.RemotePath,
		"bytes":           result.Bytes,
		"checksum_sha256": result.ChecksumSHA256,
		"duration_ms":     result.DurationMS,
	})
}

func (s fileTransferHandlers) launchTransferBatch(runtime *databaseRuntime, batchID int64, overwrite bool) {
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferBatchTimeout)
	runtime.transferJobs.Batches.Launch(batchID, cancel, func() {
		s.runTransferBatch(ctx, runtime, batchID, overwrite)
	})
}

func (s fileTransferHandlers) runTransferBatch(ctx context.Context, runtime *databaseRuntime, batchID int64, overwrite bool) {
	control := &transferjobs.Control{}
	runtime.transferJobs.Batches.RegisterControl(batchID, control)
	defer runtime.transferJobs.Batches.UnregisterControl(batchID)

	if ok, err := runtime.fileTransfers.MarkBatchRunning(ctx, batchID); err != nil {
		log.Printf("claim file transfer batch failed batch=%d error=%v", batchID, err)
		return
	} else if !ok {
		return
	}
	batch, err := runtime.fileTransfers.GetBatch(ctx, batchID)
	if err != nil {
		s.cleanupBatchTempsIfDurable(runtime, batchID, s.finishFileTransferBatchError(runtime, batchID, ctx, err))
		log.Printf("read file transfer batch before run failed batch=%d error=%v", batchID, err)
		return
	}
	if batch.Direction == filetransfer.DirectionDownload {
		if err := s.validateDownloadBatchBeforeRun(ctx, runtime, batch); err != nil {
			if classifyFileTransferInterruption(ctx, err) != fileTransferNotInterrupted {
				s.cleanupBatchTempsIfDurable(runtime, batchID, s.finishFileTransferBatchError(runtime, batchID, ctx, err))
				return
			}
			message := credentialSafeFileTransferErrorMessage(ctx, runtime, batch.RuntimeID, "file transfer batch guardrail rejected", nil, err)
			log.Printf("reject file transfer batch before run batch=%d error=%s", batchID, message)
			durable := s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
				return runtime.fileTransfers.FailBatchWithKind(ctx, batchID, message, filetransfer.FailureKindValidation)
			})
			s.cleanupBatchTempsIfDurable(runtime, batchID, durable)
			s.writeObservationAudit(context.Background(), runtime, "gateway", nil, batch.RuntimeID, "file_transfer.batch.guardrail_rejected", map[string]any{
				"batch_id": batchID,
				"error":    message,
			})
			return
		}
	}
	batch, err = runtime.fileTransfers.GetBatch(ctx, batchID)
	if err != nil {
		s.cleanupBatchTempsIfDurable(runtime, batchID, s.finishFileTransferBatchError(runtime, batchID, ctx, err))
		log.Printf("read file transfer batch failed batch=%d error=%v", batchID, err)
		return
	}
	for {
		if err := control.Wait(ctx); err != nil {
			break
		}
		latest, err := runtime.fileTransfers.NextBatchPendingItem(ctx, batchID)
		if errors.Is(err, filetransfer.ErrNotFound) {
			break
		}
		if err != nil {
			log.Printf("read next file transfer batch item failed batch=%d error=%v", batchID, err)
			s.cleanupBatchTempsIfDurable(runtime, batchID, s.finishFileTransferBatchError(runtime, batchID, ctx, err))
			return
		}
		s.runTransferBatchItem(ctx, runtime, latest.ID, overwrite, control)
		_ = runtime.fileTransfers.RecalculateBatch(context.Background(), batchID)
		if ctx.Err() != nil {
			break
		}
	}
	if ctx.Err() != nil {
		s.cleanupBatchTempsIfDurable(runtime, batchID, s.finishFileTransferBatchError(runtime, batchID, ctx, ctx.Err()))
		return
	}
	batch, err = transferjobs.PrepareFileTransferBatch(runtime.finalization.Context(), runtime.fileTransfers, batchID)
	if err != nil {
		log.Printf("prepare completed file transfer batch failed batch=%d error=%v", batchID, err)
		s.cleanupBatchTempsIfDurable(runtime, batchID, s.finishFileTransferBatchError(runtime, batchID, ctx, err))
		return
	}
	if batch.Direction == filetransfer.DirectionDownload && batch.FailedItems == 0 && batch.CompletedItems > 0 {
		if len(batch.Items) > 1 {
			archivePath, err := s.createDownloadArchive(batch)
			if err != nil {
				log.Printf("create file transfer archive failed batch=%d error=%v", batchID, err)
				message := credentialSafeFileTransferErrorMessage(context.Background(), runtime, batch.RuntimeID, "create file transfer archive failed", nil, err)
				durable := s.persistFileTransferBatchTerminal(runtime, batchID, func(ctx context.Context) (bool, error) {
					return runtime.fileTransfers.FailBatchWithKind(ctx, batchID, message, filetransfer.FailureKindUnknown)
				})
				s.cleanupBatchTempsIfDurable(runtime, batchID, durable)
				return
			}
			if !s.persistDownloadBatchArchive(runtime.finalization.Context(), runtime, batch, archivePath) {
				return
			}
			batch.ArchivePath = archivePath
		}
	}
	if err := transferjobs.FinalizeFileTransferBatch(runtime.finalization.Context(), runtime.fileTransfers, batchID); err != nil {
		log.Printf("complete file transfer batch failed batch=%d error=%v", batchID, err)
		return
	}
	if batch.Direction == filetransfer.DirectionDownload && batch.FailedItems == 0 && batch.CompletedItems > 0 {
		s.scheduleBatchTempCleanup(batch)
	}
}

func (s fileTransferHandlers) persistDownloadBatchArchive(ctx context.Context, runtime *databaseRuntime, batch filetransfer.BatchRecord, archivePath string) bool {
	attemptCtx, cancel := context.WithTimeout(ctx, fileTransferPersistenceAttemptTimeout)
	err := runtime.fileTransfers.SetBatchArchive(attemptCtx, batch.ID, archivePath)
	cancel()
	if err != nil {
		log.Printf("set file transfer archive failed batch=%d error=%v", batch.ID, err)
		message := credentialSafeFileTransferErrorMessage(context.Background(), runtime, batch.RuntimeID, "persist file transfer archive failed", nil, err)
		if s.persistDownloadBatchArchiveFailure(ctx, runtime, batch.ID, message) {
			batch.ArchivePath = archivePath
			s.scheduleBatchTempCleanup(batch)
		}
		return false
	}
	return true
}

func (s fileTransferHandlers) persistDownloadBatchArchiveFailure(ctx context.Context, runtime *databaseRuntime, batchID int64, message string) bool {
	loggedFailure := false
	retryDelay := fileTransferPersistenceRetryInterval
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, fileTransferPersistenceAttemptTimeout)
		failed, err := runtime.fileTransfers.FailBatchWithKind(attemptCtx, batchID, message, filetransfer.FailureKindLocalPersistence)
		if err == nil {
			if failed {
				cancel()
				return true
			}
			current, readErr := runtime.fileTransfers.GetBatch(attemptCtx, batchID)
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
			current, readErr := runtime.fileTransfers.GetBatch(checkCtx, batchID)
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

func (s fileTransferHandlers) validateDownloadBatchBeforeRun(ctx context.Context, runtime *databaseRuntime, batch filetransfer.BatchRecord) error {
	adapter, err := s.fileTransferAdapter(ctx, runtime, batch.RuntimeID)
	if err != nil {
		return err
	}
	sizes := make(map[int64]int64, len(batch.Items))
	ports := connectorFileTransferPortsForID(ctx, s.Server, runtime, batch.RuntimeID)
	var totalSize int64
	for _, item := range batch.Items {
		if item.Status != filetransfer.StatusPending {
			continue
		}
		status, err := adapter.StatRemotePath(ctx, ports.gateway, ports.runtime, batch.RuntimeID, item.RemotePath)
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
	return runtime.fileTransfers.UpdatePendingBatchItemSizes(ctx, batch.ID, sizes)
}

func (s fileTransferHandlers) runTransferBatchItem(ctx context.Context, runtime *databaseRuntime, transferID int64, overwrite bool, control *transferjobs.Control) {
	itemCtx, itemCancel := context.WithCancel(ctx)
	runtime.transferJobs.Files.RegisterCancel(transferID, itemCancel)
	runtime.transferJobs.Files.RegisterControl(transferID, control)
	defer runtime.transferJobs.Files.UnregisterCancel(transferID)
	defer runtime.transferJobs.Files.UnregisterControl(transferID)
	defer itemCancel()

	ok, err := runtime.fileTransfers.MarkRunning(itemCtx, transferID)
	if err != nil {
		s.finishFileTransferError(runtime, transferID, itemCtx, err)
		log.Printf("mark file transfer running failed transfer=%d error=%v", transferID, err)
		return
	}
	if !ok {
		return
	}
	item, err := runtime.fileTransfers.Get(itemCtx, transferID)
	if err != nil {
		s.finishFileTransferError(runtime, transferID, itemCtx, err)
		log.Printf("read file transfer failed transfer=%d error=%v", transferID, err)
		return
	}
	adapter, err := s.fileTransferAdapter(itemCtx, runtime, item.RuntimeID)
	if err != nil {
		s.finishFileTransferError(runtime, transferID, itemCtx, err)
		return
	}
	options := connectorapi.TransferOptions{
		Progress: s.transferProgress(runtime, transferID),
		Wait:     control.Wait,
		MaxBytes: maxFileTransferObjectBytes,
	}
	var result connectorapi.TransferResult
	ports := connectorFileTransferPortsForID(itemCtx, s.Server, runtime, item.RuntimeID)
	if item.Direction == filetransfer.DirectionUpload {
		defer s.removeTransferTemp(runtime, transferID)
		result, err = adapter.UploadFile(itemCtx, ports.gateway, ports.runtime, item.RuntimeID, item.TempPath, item.RemotePath, overwrite, options)
	} else {
		result, err = adapter.DownloadFile(itemCtx, ports.gateway, ports.runtime, item.RuntimeID, item.RemotePath, item.TempPath, options)
	}
	if err != nil {
		if item.Direction == filetransfer.DirectionDownload {
			_ = os.Remove(item.TempPath)
		}
		s.finishFileTransferError(runtime, transferID, itemCtx, err)
		return
	}
	completed, err := transferjobs.FinalizeSuccessfulFileTransfer(runtime.finalization.Context(), runtime.fileTransfers, transferID, result.Bytes, result.ChecksumSHA256)
	if err != nil {
		log.Printf("complete file transfer failed transfer=%d error=%v", transferID, err)
	}
	if !completed {
		return
	}
	if item.Direction == filetransfer.DirectionDownload && item.BatchID == 0 {
		s.scheduleTransferTempCleanup(item.TempPath)
	}
}

func (s fileTransferHandlers) transferProgress(runtime *databaseRuntime, transferID int64) connectorapi.TransferProgress {
	var lastWrite time.Time
	started := time.Now()
	return func(transferred int64, total int64) {
		now := time.Now()
		if transferred != total && now.Sub(lastWrite) < 250*time.Millisecond {
			return
		}
		lastWrite = now
		bytesPerSecond, etaSeconds := transferSpeedAndETA(transferred, total, now.Sub(started))
		if err := runtime.fileTransfers.UpdateProgressStats(context.Background(), transferID, transferred, total, bytesPerSecond, etaSeconds); err != nil {
			log.Printf("update file transfer progress failed transfer=%d error=%v", transferID, err)
		}
		item, err := runtime.fileTransfers.Get(context.Background(), transferID)
		if err == nil && item.BatchID > 0 {
			if err := runtime.fileTransfers.RecalculateBatch(context.Background(), item.BatchID); err != nil {
				log.Printf("recalculate file transfer batch progress failed batch=%d error=%v", item.BatchID, err)
			}
		}
	}
}
