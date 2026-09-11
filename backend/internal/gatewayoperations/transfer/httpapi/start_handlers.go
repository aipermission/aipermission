package filetransferhttp

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
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
)

func (s Handlers) StartUpload(w http.ResponseWriter, r *http.Request) {
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
	remotePath, execution, err := s.resolveAndNormalizeTransferPath(r.Context(), runtime, runtimeID, r.FormValue("remote_path"), false)
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

	tempPath, size, checksum, err := s.runner.StageUploadFile(file)
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
	if replay, replayErr := runtime.Storage().GetIdempotentTransfer(r.Context(), claim); replayErr == nil {
		_ = os.Remove(tempPath)
		if fileTransferCanLaunch(replay.Status) {
			if err := s.runner.LaunchUpload(r.Context(), runtime, replay.ID, overwrite, execution.runnerExecution()); err != nil {
				s.runner.RejectTransferLaunch(runtime, replay.ID)
				writeError(w, http.StatusServiceUnavailable, "file transfer could not start")
				return
			}
		}
		writeJSON(w, http.StatusAccepted, replay)
		return
	} else if !errors.Is(replayErr, filetransfer.ErrIdempotencyNotFound) {
		_ = os.Remove(tempPath)
		if !writeFileTransferIdempotencyError(w, replayErr) {
			writeInternalError(w)
		}
		return
	}
	if ok := s.checkUploadOverwrite(w, r, execution, remotePath, overwrite, tempPath); !ok {
		return
	}
	record, created, err := runtime.Storage().CreateIdempotent(r.Context(), filetransfer.CreateRequest{
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
	if fileTransferCanLaunch(record.Status) {
		if err := s.runner.LaunchUpload(r.Context(), runtime, record.ID, overwrite, execution.runnerExecution()); err != nil {
			s.runner.RejectTransferLaunch(runtime, record.ID)
			writeError(w, http.StatusServiceUnavailable, "file transfer could not start")
			return
		}
	}
	writeJSON(w, http.StatusAccepted, record)
}

func (s Handlers) StartUploadBatch(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	createdBatch, ok := s.createUploadBatchFromMultipart(w, r, runtime, filetransfer.SourceUI, nil, nil, nil)
	if !ok {
		return
	}
	if createdBatch.created {
		s.writeObservationAudit(r.Context(), runtime, "user", nil, createdBatch.runtimeID, "file_transfer.batch.upload.started", map[string]any{
			"batch_id":   createdBatch.batch.ID,
			"items":      len(createdBatch.batch.Items),
			"size_bytes": createdBatch.batch.SizeBytes,
			"overwrite":  createdBatch.overwrite,
		})
	}
	if err := s.runner.LaunchBatch(r.Context(), runtime, createdBatch.batch.ID, createdBatch.overwrite, createdBatch.execution.runnerExecution()); err != nil {
		s.runner.RejectBatchLaunch(runtime, createdBatch.batch.ID)
		writeError(w, http.StatusServiceUnavailable, "file transfer batch could not start")
		return
	}
	writeJSON(w, http.StatusAccepted, createdBatch.batch)
}

type uploadBatchCreation struct {
	batch     filetransfer.BatchRecord
	runtimeID int64
	overwrite bool
	created   bool
	execution transferExecution
}

func (s Handlers) createUploadBatchFromMultipart(w http.ResponseWriter, r *http.Request, runtime *transferapp.Runtime, source string, status *string, authorize func(runtimeID int64) bool, prepare func(runtimeID int64, remoteDir string, fileNames []string, overwrite bool) bool) (uploadBatchCreation, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileTransferBatchBytes+maxFileTransferMultipartOverhead)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload")
		return uploadBatchCreation{}, false
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	plan, ok := s.prepareUploadBatchMultipart(w, r, runtime, source, status, authorize, prepare)
	if !ok {
		return uploadBatchCreation{}, false
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
			return uploadBatchCreation{}, false
		}
		tempPath, size, checksum, err := s.runner.StageUploadFile(file)
		_ = file.Close()
		if err != nil {
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusBadRequest, err.Error())
			return uploadBatchCreation{}, false
		}
		stagedBytes, err = validateStagedUploadSize(size, stagedBytes)
		if err != nil {
			_ = os.Remove(tempPath)
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusRequestEntityTooLarge, err.Error())
			return uploadBatchCreation{}, false
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
			return uploadBatchCreation{}, false
		}
		if replay, replayErr := runtime.Storage().GetIdempotentBatch(r.Context(), claim); replayErr == nil {
			cleanupTempPaths(tempPaths)
			return uploadBatchCreation{batch: replay, runtimeID: runtimeID, overwrite: overwrite, execution: plan.execution}, true
		} else if !errors.Is(replayErr, filetransfer.ErrIdempotencyNotFound) {
			cleanupTempPaths(tempPaths)
			if !writeFileTransferIdempotencyError(w, replayErr) {
				writeInternalError(w)
			}
			return uploadBatchCreation{}, false
		}
	}
	if initialStatus != filetransfer.StatusPendingApproval {
		conflicts, ok := s.checkUploadBatchOverwrite(w, r, plan.execution, requests, overwrite, tempPaths)
		if !ok {
			if len(conflicts) > 0 {
				writeJSON(w, http.StatusConflict, remoteFileConflictsResponse{
					Error:     "one or more remote files already exist",
					Code:      "remote_files_exist",
					Conflicts: conflicts,
				})
			}
			return uploadBatchCreation{}, false
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
		batch, created, err = runtime.Storage().CreateBatchIdempotent(r.Context(), createRequest, claim)
	} else {
		batch, err = runtime.Storage().CreateBatch(r.Context(), createRequest)
	}
	if err != nil {
		cleanupTempPaths(tempPaths)
		if writeFileTransferIdempotencyError(w, err) {
			return uploadBatchCreation{}, false
		}
		writeInternalError(w)
		return uploadBatchCreation{}, false
	}
	if !created {
		cleanupTempPaths(tempPaths)
	}
	return uploadBatchCreation{batch: batch, runtimeID: runtimeID, overwrite: overwrite, created: created, execution: plan.execution}, true
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
	execution      transferExecution
}

func (s Handlers) prepareUploadBatchMultipart(w http.ResponseWriter, r *http.Request, runtime *transferapp.Runtime, source string, status *string, authorize func(runtimeID int64) bool, prepare func(runtimeID int64, remoteDir string, fileNames []string, overwrite bool) bool) (uploadBatchMultipartPlan, bool) {
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
	plan.remoteDir, plan.execution, err = s.resolveAndNormalizeTransferPath(r.Context(), runtime, plan.runtimeID, r.FormValue("remote_dir"), true)
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
	plan.fileNames = make([]string, 0, len(plan.headers))
	plan.remotePaths = make([]string, 0, len(plan.headers))
	seenRemotePaths := map[string]bool{}
	for index, header := range plan.headers {
		fileName := header.Filename
		if err := filetransfer.ValidateFileName(fileName); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return uploadBatchMultipartPlan{}, false
		}
		relativePath, err := transferUploadFilename(plan.execution.adapter, header)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid upload filename")
			return uploadBatchMultipartPlan{}, false
		}
		if len(relativePaths) > 0 {
			relativePath = relativePaths[index]
		}
		remotePath, err := transferUploadPath(plan.execution.adapter, plan.remoteDir, relativePath)
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
		return currentBatchBytes, fmt.Errorf("upload object cannot exceed %s", filetransfer.FormatByteLimit(maxFileTransferObjectBytes))
	}
	if currentBatchBytes > maxFileTransferBatchBytes-size {
		return currentBatchBytes, fmt.Errorf("upload batch cannot exceed %s total size", filetransfer.FormatByteLimit(maxFileTransferBatchBytes))
	}
	return currentBatchBytes + size, nil
}

func validateDownloadObjectSize(size int64) error {
	if size < 0 || size > maxFileTransferObjectBytes {
		return fmt.Errorf("download object cannot exceed %s", filetransfer.FormatByteLimit(maxFileTransferObjectBytes))
	}
	return nil
}

func (s Handlers) StartDownload(w http.ResponseWriter, r *http.Request) {
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
	if request.RuntimeID < 1 {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	remotePath, execution, err := s.resolveAndNormalizeTransferPath(r.Context(), runtime, request.RuntimeID, request.RemotePath, false)
	if err != nil {
		writeTransferPathError(w, err)
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
	if replay, replayErr := runtime.Storage().GetIdempotentTransfer(r.Context(), claim); replayErr == nil {
		if fileTransferCanLaunch(replay.Status) {
			if err := s.runner.LaunchDownload(r.Context(), runtime, replay.ID, execution.runnerExecution()); err != nil {
				s.runner.RejectTransferLaunch(runtime, replay.ID)
				writeError(w, http.StatusServiceUnavailable, "file transfer could not start")
				return
			}
		}
		writeJSON(w, http.StatusAccepted, replay)
		return
	} else if !errors.Is(replayErr, filetransfer.ErrIdempotencyNotFound) {
		if !writeFileTransferIdempotencyError(w, replayErr) {
			writeInternalError(w)
		}
		return
	}
	remoteStatus, err := execution.adapter.StatRemotePath(r.Context(), execution.gateway, execution.runtime, request.RuntimeID, remotePath)
	if err != nil {
		s.writeCredentialSafeConnectorError(w, execution, http.StatusBadGateway, "remote path check failed", err)
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
	tempPath, err := s.runner.ReserveDownloadTempFile()
	if err != nil {
		writeInternalError(w)
		return
	}
	fileName := safeFileName(path.Base(remotePath))
	record, created, err := runtime.Storage().CreateIdempotent(r.Context(), filetransfer.CreateRequest{
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
	if fileTransferCanLaunch(record.Status) {
		if err := s.runner.LaunchDownload(r.Context(), runtime, record.ID, execution.runnerExecution()); err != nil {
			s.runner.RejectTransferLaunch(runtime, record.ID)
			writeError(w, http.StatusServiceUnavailable, "file transfer could not start")
			return
		}
	}
	writeJSON(w, http.StatusAccepted, record)
}

func fileTransferCanLaunch(status string) bool {
	return status == filetransfer.StatusPending || status == filetransfer.StatusPaused
}

func (s Handlers) StartDownloadBatch(w http.ResponseWriter, r *http.Request) {
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
	if err := validateDownloadBatchInput(request.RuntimeID, request.RemotePaths, filetransfer.SourceUI, request.IdempotencyKey); err != nil {
		s.writeFileTransferStartError(w, r.Context(), runtime, request.RuntimeID, err)
		return
	}
	execution, err := s.resolveTransferExecution(ctx, runtime, request.RuntimeID)
	if err != nil {
		handleConnectorTargetRuntimeError(w, err)
		return
	}
	batch, created, err := s.createDownloadBatch(ctx, runtime, &execution, request.RuntimeID, request.RemotePaths, request.ArchiveName, filetransfer.SourceUI, filetransfer.StatusPending, request.IdempotencyKey)
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
		if err := s.runner.LaunchBatch(r.Context(), runtime, batch.ID, false, execution.runnerExecution()); err != nil {
			s.runner.RejectBatchLaunch(runtime, batch.ID)
			writeError(w, http.StatusServiceUnavailable, "file transfer batch could not start")
			return
		}
	}
	writeJSON(w, http.StatusAccepted, batch)
}

func (s Handlers) createDownloadBatch(ctx context.Context, runtime *transferapp.Runtime, accepted *transferExecution, runtimeID int64, remotePaths []string, archiveName string, source string, status string, idempotencyKey string) (filetransfer.BatchRecord, bool, error) {
	if err := validateDownloadBatchInput(runtimeID, remotePaths, source, idempotencyKey); err != nil {
		return filetransfer.BatchRecord{}, false, err
	}
	execution, err := s.downloadBatchExecution(ctx, runtime, accepted, runtimeID)
	if err != nil {
		return filetransfer.BatchRecord{}, false, err
	}
	plan, err := prepareDownloadBatchPlan(execution.adapter, remotePaths, archiveName)
	if err != nil {
		return filetransfer.BatchRecord{}, false, err
	}
	claim, replay, err := downloadBatchIdempotency(ctx, runtime, runtimeID, source, idempotencyKey, plan)
	if err != nil || replay != nil {
		if replay != nil {
			return *replay, false, nil
		}
		return filetransfer.BatchRecord{}, false, err
	}
	plan.assignDefaultArchiveName()
	items, tempPaths, err := s.prepareDownloadBatchItems(ctx, execution, runtimeID, plan, status != filetransfer.StatusPendingApproval)
	if err != nil {
		return filetransfer.BatchRecord{}, false, err
	}
	createRequest := filetransfer.CreateBatchRequest{
		RuntimeID:   runtimeID,
		Direction:   filetransfer.DirectionDownload,
		Source:      source,
		Status:      status,
		ArchiveName: plan.archiveName,
		Items:       items,
	}
	var batch filetransfer.BatchRecord
	created := true
	if source == filetransfer.SourceUI {
		batch, created, err = runtime.Storage().CreateBatchIdempotent(ctx, createRequest, claim)
	} else {
		batch, err = runtime.Storage().CreateBatch(ctx, createRequest)
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

// CreateAndLaunchDownloadBatch creates and synchronously registers a
// connector-owned batch before returning control to an asynchronous action.
func (s Handlers) CreateAndLaunchDownloadBatch(ctx context.Context, runtime *transferapp.Runtime, authorization connectorapi.TransferAuthorization, runtimeID int64, remotePaths []string, archiveName string, source string) (filetransfer.BatchRecord, error) {
	execution, err := s.resolveTransferExecution(ctx, runtime, runtimeID)
	if err != nil {
		return filetransfer.BatchRecord{}, err
	}
	if !execution.authorizedBy(authorization) {
		return filetransfer.BatchRecord{}, errTransferExecutionStale
	}
	batch, _, err := s.createDownloadBatch(ctx, runtime, &execution, runtimeID, remotePaths, archiveName, source, filetransfer.StatusPending, "")
	if err != nil {
		return filetransfer.BatchRecord{}, err
	}
	if err := s.runner.LaunchBatch(ctx, runtime, batch.ID, false, execution.runnerExecution()); err != nil {
		s.runner.RejectBatchLaunch(runtime, batch.ID)
		return filetransfer.BatchRecord{}, err
	}
	return batch, nil
}

func (s Handlers) writeFileTransferStartError(w http.ResponseWriter, ctx context.Context, runtime *transferapp.Runtime, runtimeID int64, err error) bool {
	if writeFileTransferIdempotencyError(w, err) {
		return true
	}
	if errors.Is(err, connectortargets.ErrTargetProfileNotFound) || errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
		handleConnectorTargetRuntimeError(w, err)
		return true
	}
	var connectorErr *fileTransferConnectorError
	if errors.As(err, &connectorErr) {
		s.writeCredentialSafeConnectorError(w, connectorErr.Execution, http.StatusBadGateway, "remote path check failed", connectorErr.Err)
		return true
	}
	var startErr *fileTransferStartError
	if errors.As(err, &startErr) {
		writeError(w, startErr.Status, startErr.Message)
		return true
	}
	return false
}

func (s Handlers) DownloadTransferredFile(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	item, err := runtime.Storage().Get(r.Context(), id)
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
	if item.TempPath == "" || !s.runner.TempPathAllowed(item.TempPath) {
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
