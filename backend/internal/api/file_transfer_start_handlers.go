package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func (s fileTransferHandlers) startUpload(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFileTransferObjectBytes+maxFileTransferMultipartOverhead)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	runtimeID, ok := parseFormInt64(w, r, "runtime_id")
	if !ok {
		return
	}
	remotePath, err := s.normalizeTransferPath(r.Context(), runtime, runtimeID, r.FormValue("remote_path"), false)
	if err != nil {
		writeTransferPathError(w, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	overwrite := parseFormBool(r, "overwrite")

	tempPath, size, err := s.stageUploadFile(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := validateStagedUploadSize(size, 0); err != nil {
		_ = os.Remove(tempPath)
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	if ok := s.checkUploadOverwrite(w, r, runtime, runtimeID, remotePath, overwrite, tempPath); !ok {
		return
	}
	fileName := safeFileName(header.Filename)
	record, err := runtime.fileTransfers.Create(r.Context(), filetransfer.CreateRequest{
		RuntimeID:  runtimeID,
		Direction:  filetransfer.DirectionUpload,
		Source:     filetransfer.SourceUI,
		LocalPath:  fileName,
		RemotePath: remotePath,
		FileName:   fileName,
		SizeBytes:  size,
		TempPath:   tempPath,
	})
	if err != nil {
		_ = os.Remove(tempPath)
		writeInternalError(w)
		return
	}
	s.writeObservationAudit(r.Context(), runtime, "user", nil, runtimeID, "file_transfer.upload.started", map[string]any{
		"transfer_id": record.ID,
		"remote_path": remotePath,
		"file_name":   fileName,
		"size_bytes":  size,
		"overwrite":   overwrite,
	})
	s.launchUpload(runtime, record.ID, overwrite)
	writeJSON(w, http.StatusAccepted, record)
}

func (s fileTransferHandlers) startUploadBatch(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	batch, runtimeID, overwrite, ok := s.createUploadBatchFromMultipart(w, r, runtime, filetransfer.SourceUI, nil, nil, nil)
	if !ok {
		return
	}
	s.writeObservationAudit(r.Context(), runtime, "user", nil, runtimeID, "file_transfer.batch.upload.started", map[string]any{
		"batch_id":   batch.ID,
		"items":      len(batch.Items),
		"size_bytes": batch.SizeBytes,
		"overwrite":  overwrite,
	})
	s.launchTransferBatch(runtime, batch.ID, overwrite)
	writeJSON(w, http.StatusAccepted, batch)
}

func (s fileTransferHandlers) createUploadBatchFromMultipart(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, source string, status *string, authorize func(runtimeID int64) bool, prepare func(runtimeID int64, remoteDir string, fileNames []string, overwrite bool) bool) (filetransfer.BatchRecord, int64, bool, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileTransferBatchBytes+maxFileTransferMultipartOverhead)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload")
		return filetransfer.BatchRecord{}, 0, false, false
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	runtimeID, ok := parseFormInt64(w, r, "runtime_id")
	if !ok {
		return filetransfer.BatchRecord{}, 0, false, false
	}
	if authorize != nil && !authorize(runtimeID) {
		return filetransfer.BatchRecord{}, 0, false, false
	}
	initialStatus := filetransfer.StatusPending
	if status != nil && strings.TrimSpace(*status) != "" {
		initialStatus = strings.TrimSpace(*status)
	}
	remoteDir, err := s.normalizeTransferPath(r.Context(), runtime, runtimeID, r.FormValue("remote_dir"), true)
	if err != nil {
		writeTransferPathError(w, err)
		return filetransfer.BatchRecord{}, 0, false, false
	}
	overwrite := parseFormBool(r, "overwrite")
	headers := r.MultipartForm.File["files"]
	if len(headers) == 0 {
		headers = r.MultipartForm.File["file"]
	}
	if len(headers) == 0 {
		writeError(w, http.StatusBadRequest, "files are required")
		return filetransfer.BatchRecord{}, 0, false, false
	}
	if len(headers) > maxFileTransferBatchItems {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot upload more than %d files at once", maxFileTransferBatchItems))
		return filetransfer.BatchRecord{}, 0, false, false
	}
	relativePaths := []string{}
	if raw := strings.TrimSpace(r.FormValue("relative_paths")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &relativePaths); err != nil || len(relativePaths) != len(headers) {
			writeError(w, http.StatusBadRequest, "relative_paths must match the uploaded files")
			return filetransfer.BatchRecord{}, 0, false, false
		}
	}
	fileNames := make([]string, 0, len(headers))
	adapter, err := s.fileTransferAdapter(r.Context(), runtime, runtimeID)
	if err != nil {
		handleConnectorTargetRuntimeError(w, err)
		return filetransfer.BatchRecord{}, 0, false, false
	}
	remotePaths := make([]string, 0, len(headers))
	seenRemotePaths := map[string]bool{}
	for index, header := range headers {
		fileName := safeFileName(header.Filename)
		relativePath, err := transferUploadFilename(adapter, header)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid upload filename")
			return filetransfer.BatchRecord{}, 0, false, false
		}
		if len(relativePaths) > 0 {
			relativePath = relativePaths[index]
		}
		remotePath, err := transferUploadPath(adapter, remoteDir, relativePath)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return filetransfer.BatchRecord{}, 0, false, false
		}
		if seenRemotePaths[remotePath] {
			writeError(w, http.StatusBadRequest, "upload queue contains duplicate remote paths")
			return filetransfer.BatchRecord{}, 0, false, false
		}
		seenRemotePaths[remotePath] = true
		fileNames = append(fileNames, fileName)
		remotePaths = append(remotePaths, remotePath)
	}
	if prepare != nil && !prepare(runtimeID, remoteDir, fileNames, overwrite) {
		return filetransfer.BatchRecord{}, 0, false, false
	}
	requests := make([]filetransfer.CreateRequest, 0, len(headers))
	tempPaths := []string{}
	var stagedBytes int64
	for i, header := range headers {
		file, err := header.Open()
		if err != nil {
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusBadRequest, "file is required")
			return filetransfer.BatchRecord{}, 0, false, false
		}
		tempPath, size, err := s.stageUploadFile(file)
		_ = file.Close()
		if err != nil {
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusBadRequest, err.Error())
			return filetransfer.BatchRecord{}, 0, false, false
		}
		stagedBytes, err = validateStagedUploadSize(size, stagedBytes)
		if err != nil {
			_ = os.Remove(tempPath)
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusRequestEntityTooLarge, err.Error())
			return filetransfer.BatchRecord{}, 0, false, false
		}
		tempPaths = append(tempPaths, tempPath)
		requests = append(requests, filetransfer.CreateRequest{
			LocalPath:  fileNames[i],
			RemotePath: remotePaths[i],
			FileName:   fileNames[i],
			SizeBytes:  size,
			TempPath:   tempPath,
		})
	}
	if initialStatus != filetransfer.StatusPendingApproval {
		conflicts, ok := s.checkUploadBatchOverwrite(w, r, runtime, runtimeID, requests, overwrite, tempPaths)
		if !ok {
			if len(conflicts) > 0 {
				writeJSON(w, http.StatusConflict, remoteFileConflictsResponse{
					Error:     "one or more remote files already exist",
					Code:      "remote_files_exist",
					Conflicts: conflicts,
				})
			}
			return filetransfer.BatchRecord{}, 0, false, false
		}
	}
	batch, err := runtime.fileTransfers.CreateBatch(r.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: runtimeID,
		Direction: filetransfer.DirectionUpload,
		Source:    source,
		Status:    initialStatus,
		Overwrite: overwrite,
		Items:     requests,
	})
	if err != nil {
		cleanupTempPaths(tempPaths)
		writeInternalError(w)
		return filetransfer.BatchRecord{}, 0, false, false
	}
	return batch, runtimeID, overwrite, true
}

func validateStagedUploadSize(size int64, currentBatchBytes int64) (int64, error) {
	if size < 0 || size > maxFileTransferObjectBytes {
		return currentBatchBytes, fmt.Errorf("upload object cannot exceed %s", formatFileTransferLimit(maxFileTransferObjectBytes))
	}
	if currentBatchBytes > maxFileTransferBatchBytes-size {
		return currentBatchBytes, fmt.Errorf("upload batch cannot exceed %s total size", formatFileTransferLimit(maxFileTransferBatchBytes))
	}
	return currentBatchBytes + size, nil
}

func validateDownloadObjectSize(size int64) error {
	if size < 0 || size > maxFileTransferObjectBytes {
		return fmt.Errorf("download object cannot exceed %s", formatFileTransferLimit(maxFileTransferObjectBytes))
	}
	return nil
}

func (s fileTransferHandlers) startDownload(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request startDownloadRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	remotePath, err := s.normalizeTransferPath(r.Context(), runtime, request.RuntimeID, request.RemotePath, false)
	if err != nil {
		writeTransferPathError(w, err)
		return
	}
	if request.RuntimeID < 1 {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	adapter, err := s.fileTransferAdapter(r.Context(), runtime, request.RuntimeID)
	if err != nil {
		handleConnectorTargetRuntimeError(w, err)
		return
	}
	remoteStatus, err := adapter.StatRemotePath(r.Context(), s.Server, runtime, request.RuntimeID, remotePath)
	if err != nil {
		s.writeCredentialSafeConnectorError(w, r.Context(), runtime, request.RuntimeID, adapter, http.StatusBadGateway, "remote path check failed", err)
		return
	}
	if !remoteStatus.Exists || remoteStatus.Type != "file" {
		writeError(w, http.StatusBadRequest, "remote path must be an existing regular file")
		return
	}
	if err := validateDownloadObjectSize(remoteStatus.Size); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	tempPath, err := s.reserveDownloadTempFile()
	if err != nil {
		writeInternalError(w)
		return
	}
	fileName := safeFileName(path.Base(remotePath))
	record, err := runtime.fileTransfers.Create(r.Context(), filetransfer.CreateRequest{
		RuntimeID:  request.RuntimeID,
		Direction:  filetransfer.DirectionDownload,
		Source:     filetransfer.SourceUI,
		RemotePath: remotePath,
		FileName:   fileName,
		SizeBytes:  remoteStatus.Size,
		TempPath:   tempPath,
	})
	if err != nil {
		_ = os.Remove(tempPath)
		writeInternalError(w)
		return
	}
	s.writeObservationAudit(r.Context(), runtime, "user", nil, request.RuntimeID, "file_transfer.download.started", map[string]any{
		"transfer_id": record.ID,
		"remote_path": remotePath,
		"file_name":   fileName,
	})
	s.launchDownload(runtime, record.ID)
	writeJSON(w, http.StatusAccepted, record)
}

func (s fileTransferHandlers) startDownloadBatch(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request startDownloadBatchRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	batch, err := s.createDownloadBatch(ctx, runtime, request.RuntimeID, request.RemotePaths, request.ArchiveName, filetransfer.SourceUI, filetransfer.StatusPending)
	if err != nil {
		if s.writeFileTransferStartError(w, r.Context(), runtime, request.RuntimeID, err) {
			return
		}
		writeInternalError(w)
		return
	}
	s.writeObservationAudit(r.Context(), runtime, "user", nil, request.RuntimeID, "file_transfer.batch.download.started", map[string]any{
		"batch_id":   batch.ID,
		"items":      len(batch.Items),
		"size_bytes": batch.SizeBytes,
	})
	s.launchTransferBatch(runtime, batch.ID, false)
	writeJSON(w, http.StatusAccepted, batch)
}

func (s fileTransferHandlers) createDownloadBatch(ctx context.Context, runtime *databaseRuntime, runtimeID int64, remotePaths []string, archiveName string, source string, status string) (filetransfer.BatchRecord, error) {
	if runtimeID < 1 {
		return filetransfer.BatchRecord{}, newFileTransferStartError(http.StatusBadRequest, "runtime_id is required")
	}
	if len(remotePaths) == 0 {
		return filetransfer.BatchRecord{}, newFileTransferStartError(http.StatusBadRequest, "remote_paths is required")
	}
	if len(remotePaths) > maxFileTransferBatchItems {
		return filetransfer.BatchRecord{}, newFileTransferStartError(http.StatusBadRequest, fmt.Sprintf("cannot download more than %d files at once", maxFileTransferBatchItems))
	}
	adapter, err := s.fileTransferAdapter(ctx, runtime, runtimeID)
	if err != nil {
		return filetransfer.BatchRecord{}, err
	}
	if policy, ok := adapter.(connectorapi.FileTransferPathPolicy); ok && (len(remotePaths) > 1 || archiveName != "") {
		if err := policy.ValidateDownloadPaths(remotePaths); err != nil {
			return filetransfer.BatchRecord{}, newFileTransferStartError(http.StatusBadRequest, err.Error())
		}
	}
	validateRemoteBeforeApproval := status != filetransfer.StatusPendingApproval
	items := make([]filetransfer.CreateRequest, 0, len(remotePaths))
	tempPaths := []string{}
	seenRemotePaths := map[string]bool{}
	var totalSize int64
	for _, raw := range remotePaths {
		remotePath, err := s.normalizeTransferPath(ctx, runtime, runtimeID, raw, false)
		if err != nil {
			cleanupTempPaths(tempPaths)
			return filetransfer.BatchRecord{}, newFileTransferStartError(http.StatusBadRequest, err.Error())
		}
		if seenRemotePaths[remotePath] {
			cleanupTempPaths(tempPaths)
			return filetransfer.BatchRecord{}, newFileTransferStartError(http.StatusBadRequest, "download queue contains duplicate remote paths")
		}
		seenRemotePaths[remotePath] = true
		var size int64
		if validateRemoteBeforeApproval {
			status, err := adapter.StatRemotePath(ctx, s.Server, runtime, runtimeID, remotePath)
			if err != nil {
				cleanupTempPaths(tempPaths)
				return filetransfer.BatchRecord{}, newFileTransferConnectorError(adapter, err)
			}
			if !status.Exists || status.Type != "file" {
				cleanupTempPaths(tempPaths)
				return filetransfer.BatchRecord{}, newFileTransferStartError(http.StatusBadRequest, "remote path must be an existing regular file")
			}
			size = status.Size
			if err := validateDownloadObjectSize(size); err != nil {
				cleanupTempPaths(tempPaths)
				return filetransfer.BatchRecord{}, newFileTransferStartError(http.StatusRequestEntityTooLarge, err.Error())
			}
		}
		totalSize += size
		if totalSize > maxFileTransferBatchBytes {
			cleanupTempPaths(tempPaths)
			return filetransfer.BatchRecord{}, newFileTransferStartError(http.StatusRequestEntityTooLarge, "download batch cannot exceed "+formatFileTransferLimit(maxFileTransferBatchBytes)+" total size")
		}
		tempPath, err := s.reserveDownloadTempFile()
		if err != nil {
			cleanupTempPaths(tempPaths)
			return filetransfer.BatchRecord{}, err
		}
		tempPaths = append(tempPaths, tempPath)
		fileName := safeFileName(path.Base(remotePath))
		items = append(items, filetransfer.CreateRequest{
			RemotePath: remotePath,
			FileName:   fileName,
			SizeBytes:  size,
			TempPath:   tempPath,
		})
	}
	cleanArchiveName := ""
	if strings.TrimSpace(archiveName) != "" {
		cleanArchiveName = safeFileName(archiveName)
	}
	if cleanArchiveName == "" && len(items) > 1 {
		cleanArchiveName = fmt.Sprintf("aipermission-download-%s.zip", time.Now().UTC().Format("20060102-150405"))
	}
	batch, err := runtime.fileTransfers.CreateBatch(ctx, filetransfer.CreateBatchRequest{
		RuntimeID:   runtimeID,
		Direction:   filetransfer.DirectionDownload,
		Source:      source,
		Status:      status,
		ArchiveName: cleanArchiveName,
		Items:       items,
	})
	if err != nil {
		cleanupTempPaths(tempPaths)
		return filetransfer.BatchRecord{}, err
	}
	return batch, nil
}

func (s fileTransferHandlers) writeFileTransferStartError(w http.ResponseWriter, ctx context.Context, runtime *databaseRuntime, runtimeID int64, err error) bool {
	if errors.Is(err, connectortargets.ErrTargetProfileNotFound) || errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
		handleConnectorTargetRuntimeError(w, err)
		return true
	}
	var connectorErr *fileTransferConnectorError
	if errors.As(err, &connectorErr) {
		s.writeCredentialSafeConnectorError(w, ctx, runtime, runtimeID, connectorErr.Adapter, http.StatusBadGateway, "remote path check failed", connectorErr.Err)
		return true
	}
	var startErr *fileTransferStartError
	if errors.As(err, &startErr) {
		writeError(w, startErr.Status, startErr.Message)
		return true
	}
	return false
}

func (s fileTransferHandlers) downloadTransferredFile(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	item, err := runtime.fileTransfers.Get(r.Context(), id)
	if errors.Is(err, filetransfer.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file transfer not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	if item.Direction != filetransfer.DirectionDownload {
		writeError(w, http.StatusBadRequest, "file transfer is not a download")
		return
	}
	if item.Status != filetransfer.StatusCompleted {
		writeError(w, http.StatusConflict, "file transfer is not completed")
		return
	}
	if item.TempPath == "" || !s.tempPathAllowed(item.TempPath) {
		writeError(w, http.StatusGone, "download file is no longer available")
		return
	}
	if _, err := os.Stat(item.TempPath); err != nil {
		writeError(w, http.StatusGone, "download file is no longer available")
		return
	}
	fileName := safeFileName(item.FileName)
	if fileName == "" {
		fileName = "aipermission-download"
	}
	setDownloadHeaders(w, fileName)
	http.ServeFile(w, r, item.TempPath)
}
