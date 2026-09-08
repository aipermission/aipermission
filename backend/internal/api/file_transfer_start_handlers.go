package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
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
	idempotencyKey := strings.TrimSpace(r.FormValue("idempotency_key"))
	if !requireFileTransferIdempotencyKey(w, idempotencyKey) {
		return
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
	fileName := header.Filename
	if err := filetransfer.ValidateFileName(fileName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tempPath, size, checksum, err := s.stageUploadFile(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := validateStagedUploadSize(size, 0); err != nil {
		_ = os.Remove(tempPath)
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	claim, err := fileTransferStartClaim(idempotencyKey, filetransfer.IdempotencyResourceTransfer, struct {
		RuntimeID  int64  `json:"runtime_id"`
		Direction  string `json:"direction"`
		RemotePath string `json:"remote_path"`
		FileName   string `json:"file_name"`
		SizeBytes  int64  `json:"size_bytes"`
		Checksum   string `json:"checksum_sha256"`
		Overwrite  bool   `json:"overwrite"`
	}{runtimeID, filetransfer.DirectionUpload, remotePath, fileName, size, checksum, overwrite})
	if err != nil {
		_ = os.Remove(tempPath)
		writeInternalError(w)
		return
	}
	if replay, replayErr := runtime.fileTransfers.GetIdempotentTransfer(r.Context(), claim); replayErr == nil {
		_ = os.Remove(tempPath)
		s.launchUpload(runtime, replay.ID, overwrite)
		writeJSON(w, http.StatusAccepted, replay)
		return
	} else if !errors.Is(replayErr, filetransfer.ErrIdempotencyNotFound) {
		_ = os.Remove(tempPath)
		if !writeFileTransferIdempotencyError(w, replayErr) {
			writeInternalError(w)
		}
		return
	}
	if ok := s.checkUploadOverwrite(w, r, runtime, runtimeID, remotePath, overwrite, tempPath); !ok {
		return
	}
	record, created, err := runtime.fileTransfers.CreateIdempotent(r.Context(), filetransfer.CreateRequest{
		RuntimeID:  runtimeID,
		Direction:  filetransfer.DirectionUpload,
		Source:     filetransfer.SourceUI,
		LocalPath:  fileName,
		RemotePath: remotePath,
		FileName:   fileName,
		SizeBytes:  size,
		TempPath:   tempPath,
	}, claim)
	if err != nil {
		_ = os.Remove(tempPath)
		if writeFileTransferIdempotencyError(w, err) {
			return
		}
		writeInternalError(w)
		return
	}
	if !created {
		_ = os.Remove(tempPath)
	} else {
		s.writeObservationAudit(r.Context(), runtime, "user", nil, runtimeID, "file_transfer.upload.started", map[string]any{
			"transfer_id": record.ID,
			"remote_path": remotePath,
			"file_name":   fileName,
			"size_bytes":  size,
			"overwrite":   overwrite,
		})
	}
	s.launchUpload(runtime, record.ID, overwrite)
	writeJSON(w, http.StatusAccepted, record)
}

func (s fileTransferHandlers) startUploadBatch(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	batch, runtimeID, overwrite, created, ok := s.createUploadBatchFromMultipart(w, r, runtime, filetransfer.SourceUI, nil, nil, nil)
	if !ok {
		return
	}
	if created {
		s.writeObservationAudit(r.Context(), runtime, "user", nil, runtimeID, "file_transfer.batch.upload.started", map[string]any{
			"batch_id":   batch.ID,
			"items":      len(batch.Items),
			"size_bytes": batch.SizeBytes,
			"overwrite":  overwrite,
		})
	}
	s.launchTransferBatch(runtime, batch.ID, overwrite)
	writeJSON(w, http.StatusAccepted, batch)
}

func (s fileTransferHandlers) createUploadBatchFromMultipart(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, source string, status *string, authorize func(runtimeID int64) bool, prepare func(runtimeID int64, remoteDir string, fileNames []string, overwrite bool) bool) (filetransfer.BatchRecord, int64, bool, bool, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileTransferBatchBytes+maxFileTransferMultipartOverhead)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload")
		return filetransfer.BatchRecord{}, 0, false, false, false
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	plan, ok := s.prepareUploadBatchMultipart(w, r, runtime, source, status, authorize, prepare)
	if !ok {
		return filetransfer.BatchRecord{}, 0, false, false, false
	}
	idempotencyKey := plan.idempotencyKey
	runtimeID := plan.runtimeID
	initialStatus := plan.initialStatus
	overwrite := plan.overwrite
	headers := plan.headers
	fileNames := plan.fileNames
	remotePaths := plan.remotePaths
	requests := make([]filetransfer.CreateRequest, 0, len(headers))
	identityItems := make([]fileTransferUploadIdentityItem, 0, len(headers))
	tempPaths := []string{}
	var stagedBytes int64
	var err error
	for i, header := range headers {
		file, err := header.Open()
		if err != nil {
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusBadRequest, "file is required")
			return filetransfer.BatchRecord{}, 0, false, false, false
		}
		tempPath, size, checksum, err := s.stageUploadFile(file)
		_ = file.Close()
		if err != nil {
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusBadRequest, err.Error())
			return filetransfer.BatchRecord{}, 0, false, false, false
		}
		stagedBytes, err = validateStagedUploadSize(size, stagedBytes)
		if err != nil {
			_ = os.Remove(tempPath)
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusRequestEntityTooLarge, err.Error())
			return filetransfer.BatchRecord{}, 0, false, false, false
		}
		tempPaths = append(tempPaths, tempPath)
		requests = append(requests, filetransfer.CreateRequest{
			LocalPath:  fileNames[i],
			RemotePath: remotePaths[i],
			FileName:   fileNames[i],
			SizeBytes:  size,
			TempPath:   tempPath,
		})
		identityItems = append(identityItems, fileTransferUploadIdentityItem{
			RemotePath: remotePaths[i], FileName: fileNames[i], SizeBytes: size, Checksum: checksum,
		})
	}
	var claim filetransfer.IdempotencyClaim
	if source == filetransfer.SourceUI {
		claim, err = fileTransferStartClaim(idempotencyKey, filetransfer.IdempotencyResourceBatch, struct {
			RuntimeID int64                            `json:"runtime_id"`
			Direction string                           `json:"direction"`
			Overwrite bool                             `json:"overwrite"`
			Items     []fileTransferUploadIdentityItem `json:"items"`
		}{runtimeID, filetransfer.DirectionUpload, overwrite, identityItems})
		if err != nil {
			cleanupTempPaths(tempPaths)
			writeInternalError(w)
			return filetransfer.BatchRecord{}, 0, false, false, false
		}
		if replay, replayErr := runtime.fileTransfers.GetIdempotentBatch(r.Context(), claim); replayErr == nil {
			cleanupTempPaths(tempPaths)
			return replay, runtimeID, overwrite, false, true
		} else if !errors.Is(replayErr, filetransfer.ErrIdempotencyNotFound) {
			cleanupTempPaths(tempPaths)
			if !writeFileTransferIdempotencyError(w, replayErr) {
				writeInternalError(w)
			}
			return filetransfer.BatchRecord{}, 0, false, false, false
		}
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
			return filetransfer.BatchRecord{}, 0, false, false, false
		}
	}
	createRequest := filetransfer.CreateBatchRequest{
		RuntimeID: runtimeID,
		Direction: filetransfer.DirectionUpload,
		Source:    source,
		Status:    initialStatus,
		Overwrite: overwrite,
		Items:     requests,
	}
	var batch filetransfer.BatchRecord
	created := true
	if source == filetransfer.SourceUI {
		batch, created, err = runtime.fileTransfers.CreateBatchIdempotent(r.Context(), createRequest, claim)
	} else {
		batch, err = runtime.fileTransfers.CreateBatch(r.Context(), createRequest)
	}
	if err != nil {
		cleanupTempPaths(tempPaths)
		if writeFileTransferIdempotencyError(w, err) {
			return filetransfer.BatchRecord{}, 0, false, false, false
		}
		writeInternalError(w)
		return filetransfer.BatchRecord{}, 0, false, false, false
	}
	if !created {
		cleanupTempPaths(tempPaths)
	}
	return batch, runtimeID, overwrite, created, true
}

type uploadBatchMultipartPlan struct {
	idempotencyKey string
	runtimeID      int64
	initialStatus  string
	remoteDir      string
	overwrite      bool
	headers        []*multipart.FileHeader
	fileNames      []string
	remotePaths    []string
}

func (s fileTransferHandlers) prepareUploadBatchMultipart(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, source string, status *string, authorize func(runtimeID int64) bool, prepare func(runtimeID int64, remoteDir string, fileNames []string, overwrite bool) bool) (uploadBatchMultipartPlan, bool) {
	plan := uploadBatchMultipartPlan{idempotencyKey: strings.TrimSpace(r.FormValue("idempotency_key"))}
	if source == filetransfer.SourceUI && !requireFileTransferIdempotencyKey(w, plan.idempotencyKey) {
		return uploadBatchMultipartPlan{}, false
	}
	var ok bool
	plan.runtimeID, ok = parseFormInt64(w, r, "runtime_id")
	if !ok || (authorize != nil && !authorize(plan.runtimeID)) {
		return uploadBatchMultipartPlan{}, false
	}
	plan.initialStatus = filetransfer.StatusPending
	if status != nil && strings.TrimSpace(*status) != "" {
		plan.initialStatus = strings.TrimSpace(*status)
	}
	var err error
	plan.remoteDir, err = s.normalizeTransferPath(r.Context(), runtime, plan.runtimeID, r.FormValue("remote_dir"), true)
	if err != nil {
		writeTransferPathError(w, err)
		return uploadBatchMultipartPlan{}, false
	}
	plan.overwrite = parseFormBool(r, "overwrite")
	plan.headers = r.MultipartForm.File["files"]
	if len(plan.headers) == 0 {
		plan.headers = r.MultipartForm.File["file"]
	}
	if len(plan.headers) == 0 {
		writeError(w, http.StatusBadRequest, "files are required")
		return uploadBatchMultipartPlan{}, false
	}
	if len(plan.headers) > maxFileTransferBatchItems {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot upload more than %d files at once", maxFileTransferBatchItems))
		return uploadBatchMultipartPlan{}, false
	}
	relativePaths := []string{}
	if raw := strings.TrimSpace(r.FormValue("relative_paths")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &relativePaths); err != nil || len(relativePaths) != len(plan.headers) {
			writeError(w, http.StatusBadRequest, "relative_paths must match the uploaded files")
			return uploadBatchMultipartPlan{}, false
		}
	}
	adapter, err := s.fileTransferAdapter(r.Context(), runtime, plan.runtimeID)
	if err != nil {
		handleConnectorTargetRuntimeError(w, err)
		return uploadBatchMultipartPlan{}, false
	}
	plan.fileNames = make([]string, 0, len(plan.headers))
	plan.remotePaths = make([]string, 0, len(plan.headers))
	seenRemotePaths := map[string]bool{}
	for index, header := range plan.headers {
		fileName := header.Filename
		if err := filetransfer.ValidateFileName(fileName); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return uploadBatchMultipartPlan{}, false
		}
		relativePath, err := transferUploadFilename(adapter, header)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid upload filename")
			return uploadBatchMultipartPlan{}, false
		}
		if len(relativePaths) > 0 {
			relativePath = relativePaths[index]
		}
		remotePath, err := transferUploadPath(adapter, plan.remoteDir, relativePath)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return uploadBatchMultipartPlan{}, false
		}
		if seenRemotePaths[remotePath] {
			writeError(w, http.StatusBadRequest, "upload queue contains duplicate remote paths")
			return uploadBatchMultipartPlan{}, false
		}
		seenRemotePaths[remotePath] = true
		plan.fileNames = append(plan.fileNames, fileName)
		plan.remotePaths = append(plan.remotePaths, remotePath)
	}
	if prepare != nil && !prepare(plan.runtimeID, plan.remoteDir, plan.fileNames, plan.overwrite) {
		return uploadBatchMultipartPlan{}, false
	}
	return plan, true
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
	if !requireFileTransferIdempotencyKey(w, request.IdempotencyKey) {
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
	claim, err := fileTransferStartClaim(request.IdempotencyKey, filetransfer.IdempotencyResourceTransfer, struct {
		RuntimeID  int64  `json:"runtime_id"`
		Direction  string `json:"direction"`
		RemotePath string `json:"remote_path"`
	}{request.RuntimeID, filetransfer.DirectionDownload, remotePath})
	if err != nil {
		writeInternalError(w)
		return
	}
	if replay, replayErr := runtime.fileTransfers.GetIdempotentTransfer(r.Context(), claim); replayErr == nil {
		s.launchDownload(runtime, replay.ID)
		writeJSON(w, http.StatusAccepted, replay)
		return
	} else if !errors.Is(replayErr, filetransfer.ErrIdempotencyNotFound) {
		if !writeFileTransferIdempotencyError(w, replayErr) {
			writeInternalError(w)
		}
		return
	}
	adapter, err := s.fileTransferAdapter(r.Context(), runtime, request.RuntimeID)
	if err != nil {
		handleConnectorTargetRuntimeError(w, err)
		return
	}
	ports := connectorFileTransferPortsForID(r.Context(), s.Server, runtime, request.RuntimeID)
	remoteStatus, err := adapter.StatRemotePath(r.Context(), ports.gateway, ports.runtime, request.RuntimeID, remotePath)
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
	record, created, err := runtime.fileTransfers.CreateIdempotent(r.Context(), filetransfer.CreateRequest{
		RuntimeID:  request.RuntimeID,
		Direction:  filetransfer.DirectionDownload,
		Source:     filetransfer.SourceUI,
		RemotePath: remotePath,
		FileName:   fileName,
		SizeBytes:  remoteStatus.Size,
		TempPath:   tempPath,
	}, claim)
	if err != nil {
		_ = os.Remove(tempPath)
		if writeFileTransferIdempotencyError(w, err) {
			return
		}
		writeInternalError(w)
		return
	}
	if !created {
		_ = os.Remove(tempPath)
	} else {
		s.writeObservationAudit(r.Context(), runtime, "user", nil, request.RuntimeID, "file_transfer.download.started", map[string]any{
			"transfer_id": record.ID,
			"remote_path": remotePath,
			"file_name":   fileName,
		})
	}
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
	batch, created, err := s.createDownloadBatch(ctx, runtime, request.RuntimeID, request.RemotePaths, request.ArchiveName, filetransfer.SourceUI, filetransfer.StatusPending, request.IdempotencyKey)
	if err != nil {
		if s.writeFileTransferStartError(w, r.Context(), runtime, request.RuntimeID, err) {
			return
		}
		writeInternalError(w)
		return
	}
	if created {
		s.writeObservationAudit(r.Context(), runtime, "user", nil, request.RuntimeID, "file_transfer.batch.download.started", map[string]any{
			"batch_id":   batch.ID,
			"items":      len(batch.Items),
			"size_bytes": batch.SizeBytes,
		})
	}
	if batch.Status == filetransfer.StatusPending || batch.Status == filetransfer.StatusPaused {
		s.launchTransferBatch(runtime, batch.ID, false)
	}
	writeJSON(w, http.StatusAccepted, batch)
}

func (s fileTransferHandlers) createDownloadBatch(ctx context.Context, runtime *databaseRuntime, runtimeID int64, remotePaths []string, archiveName string, source string, status string, idempotencyKey string) (filetransfer.BatchRecord, bool, error) {
	if runtimeID < 1 {
		return filetransfer.BatchRecord{}, false, newFileTransferStartError(http.StatusBadRequest, "runtime_id is required")
	}
	if len(remotePaths) == 0 {
		return filetransfer.BatchRecord{}, false, newFileTransferStartError(http.StatusBadRequest, "remote_paths is required")
	}
	if len(remotePaths) > maxFileTransferBatchItems {
		return filetransfer.BatchRecord{}, false, newFileTransferStartError(http.StatusBadRequest, fmt.Sprintf("cannot download more than %d files at once", maxFileTransferBatchItems))
	}
	if source == filetransfer.SourceUI && strings.TrimSpace(idempotencyKey) == "" {
		return filetransfer.BatchRecord{}, false, newFileTransferStartError(http.StatusBadRequest, "idempotency_key is required")
	}
	if len(strings.TrimSpace(idempotencyKey)) > filetransfer.MaxIdempotencyKeyBytes {
		return filetransfer.BatchRecord{}, false, newFileTransferStartError(http.StatusBadRequest, "idempotency_key is too long")
	}
	adapter, err := s.fileTransferAdapter(ctx, runtime, runtimeID)
	if err != nil {
		return filetransfer.BatchRecord{}, false, err
	}
	if policy, ok := adapter.(connectorapi.FileTransferPathPolicy); ok && (len(remotePaths) > 1 || archiveName != "") {
		if err := policy.ValidateDownloadPaths(remotePaths); err != nil {
			return filetransfer.BatchRecord{}, false, newFileTransferStartError(http.StatusBadRequest, err.Error())
		}
	}
	normalizedPaths := make([]string, 0, len(remotePaths))
	fileNames := make([]string, 0, len(remotePaths))
	seenRemotePaths := map[string]bool{}
	for _, raw := range remotePaths {
		remotePath, err := s.normalizeTransferPath(ctx, runtime, runtimeID, raw, false)
		if err != nil {
			return filetransfer.BatchRecord{}, false, newFileTransferStartError(http.StatusBadRequest, err.Error())
		}
		if seenRemotePaths[remotePath] {
			return filetransfer.BatchRecord{}, false, newFileTransferStartError(http.StatusBadRequest, "download queue contains duplicate remote paths")
		}
		fileName := safeFileName(path.Base(remotePath))
		if strings.TrimSpace(fileName) == "" {
			fileName = "aipermission-file"
		}
		if err := filetransfer.ValidateFileName(fileName); err != nil {
			return filetransfer.BatchRecord{}, false, newFileTransferStartError(http.StatusBadRequest, "remote path cannot be represented as a local filename")
		}
		seenRemotePaths[remotePath] = true
		normalizedPaths = append(normalizedPaths, remotePath)
		fileNames = append(fileNames, fileName)
	}
	cleanArchiveName := ""
	if strings.TrimSpace(archiveName) != "" {
		cleanArchiveName = safeFileName(archiveName)
	}
	var claim filetransfer.IdempotencyClaim
	if source == filetransfer.SourceUI {
		claim, err = fileTransferStartClaim(idempotencyKey, filetransfer.IdempotencyResourceBatch, struct {
			RuntimeID   int64    `json:"runtime_id"`
			Direction   string   `json:"direction"`
			RemotePaths []string `json:"remote_paths"`
			ArchiveName string   `json:"archive_name"`
		}{runtimeID, filetransfer.DirectionDownload, normalizedPaths, cleanArchiveName})
		if err != nil {
			return filetransfer.BatchRecord{}, false, err
		}
		if replay, replayErr := runtime.fileTransfers.GetIdempotentBatch(ctx, claim); replayErr == nil {
			return replay, false, nil
		} else if !errors.Is(replayErr, filetransfer.ErrIdempotencyNotFound) {
			return filetransfer.BatchRecord{}, false, replayErr
		}
	}
	if cleanArchiveName == "" && len(normalizedPaths) > 1 {
		cleanArchiveName = fmt.Sprintf("aipermission-download-%s.zip", time.Now().UTC().Format("20060102-150405"))
	}
	validateRemoteBeforeApproval := status != filetransfer.StatusPendingApproval
	items := make([]filetransfer.CreateRequest, 0, len(normalizedPaths))
	tempPaths := []string{}
	ports := connectorFileTransferPortsForID(ctx, s.Server, runtime, runtimeID)
	var totalSize int64
	for index, remotePath := range normalizedPaths {
		var size int64
		if validateRemoteBeforeApproval {
			status, err := adapter.StatRemotePath(ctx, ports.gateway, ports.runtime, runtimeID, remotePath)
			if err != nil {
				cleanupTempPaths(tempPaths)
				return filetransfer.BatchRecord{}, false, newFileTransferConnectorError(adapter, err)
			}
			if !status.Exists || status.Type != "file" {
				cleanupTempPaths(tempPaths)
				return filetransfer.BatchRecord{}, false, newFileTransferStartError(http.StatusBadRequest, "remote path must be an existing regular file")
			}
			size = status.Size
			if err := validateDownloadObjectSize(size); err != nil {
				cleanupTempPaths(tempPaths)
				return filetransfer.BatchRecord{}, false, newFileTransferStartError(http.StatusRequestEntityTooLarge, err.Error())
			}
		}
		totalSize += size
		if totalSize > maxFileTransferBatchBytes {
			cleanupTempPaths(tempPaths)
			return filetransfer.BatchRecord{}, false, newFileTransferStartError(http.StatusRequestEntityTooLarge, "download batch cannot exceed "+formatFileTransferLimit(maxFileTransferBatchBytes)+" total size")
		}
		tempPath, err := s.reserveDownloadTempFile()
		if err != nil {
			cleanupTempPaths(tempPaths)
			return filetransfer.BatchRecord{}, false, err
		}
		tempPaths = append(tempPaths, tempPath)
		items = append(items, filetransfer.CreateRequest{
			RemotePath: remotePath,
			FileName:   fileNames[index],
			SizeBytes:  size,
			TempPath:   tempPath,
		})
	}
	createRequest := filetransfer.CreateBatchRequest{
		RuntimeID:   runtimeID,
		Direction:   filetransfer.DirectionDownload,
		Source:      source,
		Status:      status,
		ArchiveName: cleanArchiveName,
		Items:       items,
	}
	var batch filetransfer.BatchRecord
	created := true
	if source == filetransfer.SourceUI {
		batch, created, err = runtime.fileTransfers.CreateBatchIdempotent(ctx, createRequest, claim)
	} else {
		batch, err = runtime.fileTransfers.CreateBatch(ctx, createRequest)
	}
	if err != nil {
		cleanupTempPaths(tempPaths)
		return filetransfer.BatchRecord{}, false, err
	}
	if !created {
		cleanupTempPaths(tempPaths)
	}
	return batch, created, nil
}

func (s fileTransferHandlers) writeFileTransferStartError(w http.ResponseWriter, ctx context.Context, runtime *databaseRuntime, runtimeID int64, err error) bool {
	if writeFileTransferIdempotencyError(w, err) {
		return true
	}
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
