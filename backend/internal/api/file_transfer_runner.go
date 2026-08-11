package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func (s fileTransferHandlers) runUpload(runtime *databaseRuntime, transferID int64, overwrite bool) {
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferTimeout)
	runtime.registerTransferCancel(transferID, cancel)
	defer runtime.unregisterTransferCancel(transferID)
	defer cancel()
	defer s.removeTransferTemp(runtime, transferID)
	ok, err := runtime.fileTransfers.MarkRunning(ctx, transferID)
	if err != nil {
		log.Printf("mark file upload running failed transfer=%d error=%v", transferID, err)
		return
	}
	if !ok {
		return
	}
	item, err := runtime.fileTransfers.Get(ctx, transferID)
	if err != nil {
		log.Printf("read file upload failed transfer=%d error=%v", transferID, err)
		return
	}
	adapter, err := s.fileTransferAdapter(ctx, runtime, item.RuntimeID)
	if err != nil {
		s.failFileTransfer(runtime, transferID, err)
		return
	}
	result, err := adapter.UploadFile(ctx, s.Server, runtime, item.RuntimeID, item.TempPath, item.RemotePath, overwrite, connectorapi.TransferOptions{
		Progress: s.transferProgress(runtime, transferID),
	})
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			s.cancelFileTransferRecord(runtime, transferID, "canceled by local user")
			return
		}
		s.failFileTransfer(runtime, transferID, err)
		return
	}
	completed, err := runtime.fileTransfers.Complete(context.Background(), transferID, result.Bytes, result.ChecksumSHA256)
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

func (s fileTransferHandlers) runDownload(runtime *databaseRuntime, transferID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferTimeout)
	runtime.registerTransferCancel(transferID, cancel)
	defer runtime.unregisterTransferCancel(transferID)
	defer cancel()
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
		log.Printf("read file download failed transfer=%d error=%v", transferID, err)
		return
	}
	adapter, err := s.fileTransferAdapter(ctx, runtime, item.RuntimeID)
	if err != nil {
		s.failFileTransfer(runtime, transferID, err)
		return
	}
	result, err := adapter.DownloadFile(ctx, s.Server, runtime, item.RuntimeID, item.RemotePath, item.TempPath, connectorapi.TransferOptions{
		Progress: s.transferProgress(runtime, transferID),
		MaxBytes: maxFileTransferObjectBytes,
	})
	if err != nil {
		_ = os.Remove(item.TempPath)
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			s.cancelFileTransferRecord(runtime, transferID, "canceled by local user")
			return
		}
		s.failFileTransfer(runtime, transferID, err)
		return
	}
	completed, err := runtime.fileTransfers.Complete(context.Background(), transferID, result.Bytes, result.ChecksumSHA256)
	if err != nil {
		log.Printf("complete file download failed transfer=%d error=%v", transferID, err)
	}
	if !completed {
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

func (s fileTransferHandlers) runTransferBatch(runtime *databaseRuntime, batchID int64, overwrite bool) {
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferBatchTimeout)
	control := newTransferControl()
	runtime.registerBatchCancel(batchID, cancel)
	runtime.registerBatchControl(batchID, control)
	defer runtime.unregisterBatchCancel(batchID)
	defer runtime.unregisterBatchControl(batchID)
	defer cancel()

	batch, err := runtime.fileTransfers.GetBatch(ctx, batchID)
	if err != nil {
		log.Printf("read file transfer batch before run failed batch=%d error=%v", batchID, err)
		return
	}
	if batch.Direction == filetransfer.DirectionDownload {
		if err := s.validateDownloadBatchBeforeRun(ctx, runtime, batch); err != nil {
			message := fileTransferFailureMessage(err)
			log.Printf("reject file transfer batch before run batch=%d error=%v", batchID, err)
			_, _ = runtime.fileTransfers.CancelBatch(context.Background(), batchID, message)
			s.cleanupBatchTemps(runtime, batchID)
			s.writeObservationAudit(context.Background(), runtime, "gateway", nil, batch.RuntimeID, "file_transfer.batch.guardrail_rejected", map[string]any{
				"batch_id": batchID,
				"error":    message,
			})
			return
		}
	}
	if ok, err := runtime.fileTransfers.MarkBatchRunning(ctx, batchID); err != nil {
		log.Printf("mark file transfer batch running failed batch=%d error=%v", batchID, err)
		return
	} else if !ok {
		return
	}
	batch, err = runtime.fileTransfers.GetBatch(ctx, batchID)
	if err != nil {
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
			break
		}
		s.runTransferBatchItem(ctx, runtime, latest.ID, overwrite, control)
		_ = runtime.fileTransfers.RecalculateBatch(context.Background(), batchID)
		if ctx.Err() != nil {
			break
		}
	}
	if ctx.Err() != nil {
		_, _ = runtime.fileTransfers.CancelBatch(context.Background(), batchID, "canceled by local user")
		s.cleanupBatchTemps(runtime, batchID)
		return
	}
	if err := runtime.fileTransfers.RecalculateBatch(context.Background(), batchID); err != nil {
		log.Printf("recalculate file transfer batch failed batch=%d error=%v", batchID, err)
	}
	batch, err = runtime.fileTransfers.GetBatch(context.Background(), batchID)
	if err != nil {
		log.Printf("read completed file transfer batch failed batch=%d error=%v", batchID, err)
		return
	}
	if batch.Direction == filetransfer.DirectionDownload && batch.FailedItems == 0 && batch.CompletedItems > 0 {
		if len(batch.Items) > 1 {
			archivePath, err := s.createDownloadArchive(batch)
			if err != nil {
				log.Printf("create file transfer archive failed batch=%d error=%v", batchID, err)
				_, _ = runtime.fileTransfers.CancelBatch(context.Background(), batchID, fileTransferFailureMessage(err))
				s.cleanupBatchTemps(runtime, batchID)
				return
			}
			if err := runtime.fileTransfers.SetBatchArchive(context.Background(), batchID, archivePath); err != nil {
				log.Printf("set file transfer archive failed batch=%d error=%v", batchID, err)
			}
			s.scheduleTransferTempCleanup(archivePath)
		}
		s.scheduleBatchItemTempCleanup(batch)
	}
	if ok, err := runtime.fileTransfers.CompleteBatch(context.Background(), batchID); err != nil {
		log.Printf("complete file transfer batch failed batch=%d error=%v", batchID, err)
	} else if ok {
		s.writeObservationAudit(context.Background(), runtime, "user", nil, batch.RuntimeID, "file_transfer.batch.completed", map[string]any{
			"batch_id":        batchID,
			"direction":       batch.Direction,
			"items":           len(batch.Items),
			"completed_items": batch.CompletedItems,
			"failed_items":    batch.FailedItems,
			"canceled_items":  batch.CanceledItems,
		})
	}
}

func (s fileTransferHandlers) validateDownloadBatchBeforeRun(ctx context.Context, runtime *databaseRuntime, batch filetransfer.BatchRecord) error {
	adapter, err := s.fileTransferAdapter(ctx, runtime, batch.RuntimeID)
	if err != nil {
		return err
	}
	sizes := make(map[int64]int64, len(batch.Items))
	var totalSize int64
	for _, item := range batch.Items {
		if item.Status != filetransfer.StatusPending {
			continue
		}
		status, err := adapter.StatRemotePath(ctx, s.Server, runtime, batch.RuntimeID, item.RemotePath)
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
			return fmt.Errorf("download batch cannot exceed 1 GiB total size")
		}
		totalSize += status.Size
		sizes[item.ID] = status.Size
	}
	if len(sizes) == 0 {
		return fmt.Errorf("download batch has no pending items")
	}
	return runtime.fileTransfers.UpdatePendingBatchItemSizes(ctx, batch.ID, sizes)
}

func (s fileTransferHandlers) runTransferBatchItem(ctx context.Context, runtime *databaseRuntime, transferID int64, overwrite bool, control *transferControl) {
	itemCtx, itemCancel := context.WithCancel(ctx)
	runtime.registerTransferCancel(transferID, itemCancel)
	runtime.registerTransferControl(transferID, control)
	defer runtime.unregisterTransferCancel(transferID)
	defer runtime.unregisterTransferControl(transferID)
	defer itemCancel()

	ok, err := runtime.fileTransfers.MarkRunning(itemCtx, transferID)
	if err != nil {
		log.Printf("mark file transfer running failed transfer=%d error=%v", transferID, err)
		return
	}
	if !ok {
		return
	}
	item, err := runtime.fileTransfers.Get(itemCtx, transferID)
	if err != nil {
		log.Printf("read file transfer failed transfer=%d error=%v", transferID, err)
		return
	}
	adapter, err := s.fileTransferAdapter(itemCtx, runtime, item.RuntimeID)
	if err != nil {
		s.failFileTransfer(runtime, transferID, err)
		return
	}
	options := connectorapi.TransferOptions{
		Progress: s.transferProgress(runtime, transferID),
		Wait:     control.Wait,
		MaxBytes: maxFileTransferObjectBytes,
	}
	var result connectorapi.TransferResult
	if item.Direction == filetransfer.DirectionUpload {
		defer s.removeTransferTemp(runtime, transferID)
		result, err = adapter.UploadFile(itemCtx, s.Server, runtime, item.RuntimeID, item.TempPath, item.RemotePath, overwrite, options)
	} else {
		result, err = adapter.DownloadFile(itemCtx, s.Server, runtime, item.RuntimeID, item.RemotePath, item.TempPath, options)
	}
	if err != nil {
		if itemCtx.Err() != nil || errors.Is(err, context.Canceled) {
			s.cancelFileTransferRecord(runtime, transferID, "canceled by local user")
			return
		}
		if item.Direction == filetransfer.DirectionDownload {
			_ = os.Remove(item.TempPath)
		}
		s.failFileTransfer(runtime, transferID, err)
		return
	}
	completed, err := runtime.fileTransfers.Complete(context.Background(), transferID, result.Bytes, result.ChecksumSHA256)
	if err != nil {
		log.Printf("complete file transfer failed transfer=%d error=%v", transferID, err)
	}
	if !completed {
		return
	}
	if item.Direction == filetransfer.DirectionDownload && item.BatchID == 0 {
		s.scheduleTransferTempCleanup(item.TempPath)
	}
	s.writeObservationAudit(context.Background(), runtime, "user", nil, item.RuntimeID, "file_transfer.completed", map[string]any{
		"transfer_id":     transferID,
		"batch_id":        item.BatchID,
		"direction":       item.Direction,
		"remote_path":     item.RemotePath,
		"bytes":           result.Bytes,
		"checksum_sha256": result.ChecksumSHA256,
		"duration_ms":     result.DurationMS,
	})
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

func (s fileTransferHandlers) checkUploadOverwrite(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, runtimeID int64, remotePath string, overwrite bool, tempPath string) bool {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	adapter, err := s.fileTransferAdapter(ctx, runtime, runtimeID)
	if err != nil {
		_ = os.Remove(tempPath)
		handleConnectorTargetRuntimeError(w, err)
		return false
	}
	status, err := adapter.StatRemotePath(ctx, s.Server, runtime, runtimeID, remotePath)
	if err != nil {
		_ = os.Remove(tempPath)
		if writeConnectorError(w, adapter, err) {
			return false
		}
		writeError(w, http.StatusBadGateway, connectorErrorMessage(adapter, "remote path check failed", err))
		return false
	}
	if !status.Exists {
		return true
	}
	if status.Type != "file" {
		_ = os.Remove(tempPath)
		writeJSON(w, http.StatusConflict, remoteFileExistsResponse{
			Error:      "remote path already exists and is not a regular file",
			Code:       "remote_path_exists",
			RemotePath: remotePath,
			Type:       status.Type,
			Size:       status.Size,
		})
		return false
	}
	if !overwrite {
		_ = os.Remove(tempPath)
		writeJSON(w, http.StatusConflict, remoteFileExistsResponse{
			Error:      "remote file already exists",
			Code:       "remote_file_exists",
			RemotePath: remotePath,
			Type:       status.Type,
			Size:       status.Size,
		})
		return false
	}
	return true
}

func (s fileTransferHandlers) checkUploadBatchOverwrite(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, runtimeID int64, requests []filetransfer.CreateRequest, overwrite bool, tempPaths []string) ([]remoteFileConflict, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	adapter, err := s.fileTransferAdapter(ctx, runtime, runtimeID)
	if err != nil {
		cleanupTempPaths(tempPaths)
		handleConnectorTargetRuntimeError(w, err)
		return nil, false
	}
	var conflicts []remoteFileConflict
	for _, item := range requests {
		status, err := adapter.StatRemotePath(ctx, s.Server, runtime, runtimeID, item.RemotePath)
		if err != nil {
			cleanupTempPaths(tempPaths)
			if writeConnectorError(w, adapter, err) {
				return nil, false
			}
			writeError(w, http.StatusBadGateway, connectorErrorMessage(adapter, "remote path check failed", err))
			return nil, false
		}
		if !status.Exists {
			continue
		}
		if status.Type != "file" {
			cleanupTempPaths(tempPaths)
			writeJSON(w, http.StatusConflict, remoteFileConflictsResponse{
				Error: "one or more remote paths already exist and are not regular files",
				Code:  "remote_paths_exist",
				Conflicts: []remoteFileConflict{{
					RemotePath: item.RemotePath,
					Type:       status.Type,
					Size:       status.Size,
				}},
			})
			return nil, false
		}
		if !overwrite {
			conflicts = append(conflicts, remoteFileConflict{RemotePath: item.RemotePath, Type: status.Type, Size: status.Size})
		}
	}
	if len(conflicts) > 0 {
		cleanupTempPaths(tempPaths)
		return conflicts, false
	}
	return nil, true
}

func (s fileTransferHandlers) failFileTransfer(runtime *databaseRuntime, transferID int64, err error) {
	message := fileTransferFailureMessage(err)
	changed, writeErr := runtime.fileTransfers.Fail(context.Background(), transferID, message)
	if writeErr != nil {
		log.Printf("fail file transfer failed transfer=%d error=%v", transferID, writeErr)
	}
	if !changed {
		return
	}
	item, readErr := runtime.fileTransfers.Get(context.Background(), transferID)
	if readErr == nil {
		s.writeObservationAudit(context.Background(), runtime, "user", nil, item.RuntimeID, "file_transfer.failed", map[string]any{
			"transfer_id": transferID,
			"direction":   item.Direction,
			"remote_path": item.RemotePath,
			"error":       message,
		})
	}
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
	item, readErr := runtime.fileTransfers.Get(context.Background(), transferID)
	if readErr == nil {
		s.writeObservationAudit(context.Background(), runtime, "user", nil, item.RuntimeID, "file_transfer.canceled", map[string]any{
			"transfer_id": transferID,
			"direction":   item.Direction,
			"remote_path": item.RemotePath,
		})
	}
}

func fileTransferFailureMessage(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("file transfer failed: %v", err)
}
