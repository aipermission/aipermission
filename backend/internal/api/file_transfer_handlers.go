package api

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/execution"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/servers"
	"github.com/aipermission/aipermission/backend/internal/sshkeys"
)

const (
	maxFileTransferUploadBytes = 512 << 20
	maxFileTransferBatchBytes  = 1 << 30
	fileTransferTimeout        = 2 * time.Hour
	fileTransferBatchTimeout   = 6 * time.Hour
	fileTransferTempTTL        = 30 * time.Minute
)

type startDownloadRequest struct {
	ServerID   int64  `json:"server_id"`
	RemotePath string `json:"remote_path"`
}

type startDownloadBatchRequest struct {
	ServerID    int64    `json:"server_id"`
	RemotePaths []string `json:"remote_paths"`
	ArchiveName string   `json:"archive_name"`
}

type updateFileTransferBatchQueueRequest struct {
	ItemIDs []int64 `json:"item_ids"`
}

type browseRemoteFilesRequest struct {
	ServerID int64  `json:"server_id"`
	Path     string `json:"path"`
}

type browseRemoteFilesResponse struct {
	Path    string                      `json:"path"`
	Parent  string                      `json:"parent"`
	Entries []execution.RemoteFileEntry `json:"entries"`
}

type remoteFileExistsResponse struct {
	Error      string `json:"error"`
	Code       string `json:"code"`
	RemotePath string `json:"remote_path"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
}

type remoteFileConflict struct {
	RemotePath string `json:"remote_path"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
}

type remoteFileConflictsResponse struct {
	Error     string               `json:"error"`
	Code      string               `json:"code"`
	Conflicts []remoteFileConflict `json:"conflicts"`
}

func (s fileTransferHandlers) listFileTransfers(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	page, err := parsePageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter := filetransfer.ListFilter{
		Direction: strings.TrimSpace(r.URL.Query().Get("direction")),
		Status:    strings.TrimSpace(r.URL.Query().Get("status")),
		Query:     page.Query,
		Limit:     page.Limit,
		Offset:    page.Offset,
	}
	if filter.Direction != "" && filter.Direction != filetransfer.DirectionUpload && filter.Direction != filetransfer.DirectionDownload {
		writeError(w, http.StatusBadRequest, "invalid direction")
		return
	}
	if filter.Status != "" && !validFileTransferStatus(filter.Status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if rawServerID := strings.TrimSpace(r.URL.Query().Get("server_id")); rawServerID != "" {
		id, ok := parseInt64Query(w, rawServerID, "server_id")
		if !ok {
			return
		}
		filter.ServerID = id
	}

	items, total, err := runtime.fileTransfers.List(r.Context(), filter)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, makePageResponse(items, total, page))
}

func (s fileTransferHandlers) getFileTransfer(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, item)
}

func (s fileTransferHandlers) browseRemoteFiles(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request browseRemoteFilesRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if request.ServerID < 1 {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	remotePath, err := normalizeRemoteDirectoryPath(request.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	server, privateKey, err := s.serverSSHMaterialFromRuntime(ctx, runtime, request.ServerID)
	if err != nil {
		handleServerSSHMaterialError(w, err)
		return
	}
	entries, err := execution.ListRemoteDirectory(ctx, s.executionTarget(server, privateKey), remotePath)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeError(w, http.StatusBadGateway, sshConnectionFailureMessage(err))
		return
	}
	parent := path.Dir(remotePath)
	if parent == "." || parent == remotePath {
		parent = "/"
	}
	writeJSON(w, http.StatusOK, browseRemoteFilesResponse{
		Path:    remotePath,
		Parent:  parent,
		Entries: entries,
	})
}

func (s fileTransferHandlers) cancelFileTransfer(w http.ResponseWriter, r *http.Request) {
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
	if item.Status != filetransfer.StatusPending && item.Status != filetransfer.StatusRunning && item.Status != filetransfer.StatusPaused {
		writeError(w, http.StatusConflict, "file transfer is not running")
		return
	}
	runtime.cancelTransfer(id)
	changed, err := runtime.fileTransfers.Cancel(context.Background(), id, "canceled by local user")
	if err != nil {
		writeInternalError(w)
		return
	}
	if changed {
		s.removeTransferTemp(runtime, id)
		s.writeAudit(context.Background(), runtime, "user", nil, item.ServerID, "file_transfer.canceled", map[string]any{
			"transfer_id": id,
			"direction":   item.Direction,
			"remote_path": item.RemotePath,
		})
	}
	updated, err := runtime.fileTransfers.Get(r.Context(), id)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s fileTransferHandlers) getFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	item, err := runtime.fileTransfers.GetBatch(r.Context(), id)
	if errors.Is(err, filetransfer.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file transfer batch not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s fileTransferHandlers) pauseFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	control := runtime.batchControl(id)
	if control == nil {
		writeError(w, http.StatusConflict, "file transfer batch is not active")
		return
	}
	if !control.Pause() {
		writeError(w, http.StatusConflict, "file transfer batch is already paused")
		return
	}
	changed, err := runtime.fileTransfers.PauseBatch(context.Background(), id)
	if err != nil {
		control.Resume()
		writeInternalError(w)
		return
	}
	if !changed {
		control.Resume()
		writeError(w, http.StatusConflict, "file transfer batch is not running")
		return
	}
	s.writeAudit(context.Background(), runtime, "user", nil, 0, "file_transfer.batch.paused", map[string]any{"batch_id": id})
	item, err := runtime.fileTransfers.GetBatch(r.Context(), id)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s fileTransferHandlers) resumeFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	control := runtime.batchControl(id)
	if control == nil {
		writeError(w, http.StatusConflict, "file transfer batch is not active")
		return
	}
	changed, err := runtime.fileTransfers.ResumeBatch(context.Background(), id)
	if err != nil {
		writeInternalError(w)
		return
	}
	if !changed {
		writeError(w, http.StatusConflict, "file transfer batch is not paused")
		return
	}
	control.Resume()
	s.writeAudit(context.Background(), runtime, "user", nil, 0, "file_transfer.batch.resumed", map[string]any{"batch_id": id})
	item, err := runtime.fileTransfers.GetBatch(r.Context(), id)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s fileTransferHandlers) cancelFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	runtime.cancelBatch(id)
	if control := runtime.batchControl(id); control != nil {
		control.Resume()
	}
	changed, err := runtime.fileTransfers.CancelBatch(context.Background(), id, "canceled by local user")
	if err != nil {
		writeInternalError(w)
		return
	}
	if changed {
		s.cleanupBatchTemps(runtime, id)
		s.writeAudit(context.Background(), runtime, "user", nil, 0, "file_transfer.batch.canceled", map[string]any{"batch_id": id})
	}
	item, err := runtime.fileTransfers.GetBatch(r.Context(), id)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s fileTransferHandlers) updateFileTransferBatchQueue(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request updateFileTransferBatchQueueRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	removed, err := runtime.fileTransfers.UpdatePausedBatchQueue(r.Context(), id, request.ItemIDs)
	if errors.Is(err, filetransfer.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file transfer batch not found")
		return
	}
	if errors.Is(err, filetransfer.ErrInvalidState) {
		writeError(w, http.StatusConflict, "file transfer batch queue can only edit pending items while paused")
		return
	}
	if errors.Is(err, filetransfer.ErrInvalidArgument) {
		writeError(w, http.StatusBadRequest, "item_ids must contain unique positive pending item ids")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	for _, item := range removed {
		if item.TempPath != "" && s.tempPathAllowed(item.TempPath) {
			_ = os.Remove(item.TempPath)
		}
	}
	s.writeAudit(context.Background(), runtime, "user", nil, 0, "file_transfer.batch.queue_updated", map[string]any{
		"batch_id": id,
		"items":    len(request.ItemIDs),
		"removed":  len(removed),
	})
	item, err := runtime.fileTransfers.GetBatch(r.Context(), id)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s fileTransferHandlers) downloadFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	batch, err := runtime.fileTransfers.GetBatch(r.Context(), id)
	if errors.Is(err, filetransfer.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file transfer batch not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	if batch.Direction != filetransfer.DirectionDownload {
		writeError(w, http.StatusBadRequest, "file transfer batch is not a download")
		return
	}
	if batch.Status != filetransfer.StatusCompleted {
		writeError(w, http.StatusConflict, "file transfer batch is not completed")
		return
	}
	var servePath string
	fileName := safeFileName(batch.ArchiveName)
	if len(batch.Items) == 1 {
		servePath = batch.Items[0].TempPath
		fileName = safeFileName(batch.Items[0].FileName)
	} else {
		servePath = batch.ArchivePath
		if fileName == "" {
			fileName = fmt.Sprintf("aipermission-download-%d.zip", batch.ID)
		}
	}
	if fileName == "" {
		fileName = "aipermission-download"
	}
	if servePath == "" || !s.tempPathAllowed(servePath) {
		writeError(w, http.StatusGone, "download file is no longer available")
		return
	}
	if _, err := os.Stat(servePath); err != nil {
		writeError(w, http.StatusGone, "download file is no longer available")
		return
	}
	setDownloadHeaders(w, fileName)
	http.ServeFile(w, r, servePath)
}

func (s fileTransferHandlers) startUpload(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFileTransferUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	serverID, ok := parseFormInt64(w, r, "server_id")
	if !ok {
		return
	}
	remotePath, err := normalizeRemoteFilePath(r.FormValue("remote_path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	if ok := s.checkUploadOverwrite(w, r, runtime, serverID, remotePath, overwrite, tempPath); !ok {
		return
	}
	fileName := safeFileName(header.Filename)
	record, err := runtime.fileTransfers.Create(r.Context(), filetransfer.CreateRequest{
		ServerID:   serverID,
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
	s.writeAudit(r.Context(), runtime, "user", nil, serverID, "file_transfer.upload.started", map[string]any{
		"transfer_id": record.ID,
		"remote_path": remotePath,
		"file_name":   fileName,
		"size_bytes":  size,
		"overwrite":   overwrite,
	})
	go s.runUpload(runtime, record.ID, overwrite)
	writeJSON(w, http.StatusAccepted, record)
}

func (s fileTransferHandlers) startUploadBatch(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFileTransferUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	serverID, ok := parseFormInt64(w, r, "server_id")
	if !ok {
		return
	}
	remoteDir, err := normalizeRemoteDirectoryPath(r.FormValue("remote_dir"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	overwrite := parseFormBool(r, "overwrite")
	headers := r.MultipartForm.File["files"]
	if len(headers) == 0 {
		headers = r.MultipartForm.File["file"]
	}
	if len(headers) == 0 {
		writeError(w, http.StatusBadRequest, "files are required")
		return
	}
	if len(headers) > 100 {
		writeError(w, http.StatusBadRequest, "cannot upload more than 100 files at once")
		return
	}
	requests := make([]filetransfer.CreateRequest, 0, len(headers))
	tempPaths := []string{}
	seenRemotePaths := map[string]bool{}
	for _, header := range headers {
		file, err := header.Open()
		if err != nil {
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusBadRequest, "file is required")
			return
		}
		tempPath, size, err := s.stageUploadFile(file)
		_ = file.Close()
		if err != nil {
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		tempPaths = append(tempPaths, tempPath)
		fileName := safeFileName(header.Filename)
		remotePath := joinRemoteFilePath(remoteDir, fileName)
		if seenRemotePaths[remotePath] {
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusBadRequest, "upload queue contains duplicate remote paths")
			return
		}
		seenRemotePaths[remotePath] = true
		requests = append(requests, filetransfer.CreateRequest{
			LocalPath:  fileName,
			RemotePath: remotePath,
			FileName:   fileName,
			SizeBytes:  size,
			TempPath:   tempPath,
		})
	}
	conflicts, ok := s.checkUploadBatchOverwrite(w, r, runtime, serverID, requests, overwrite, tempPaths)
	if !ok {
		if len(conflicts) > 0 {
			writeJSON(w, http.StatusConflict, remoteFileConflictsResponse{
				Error:     "one or more remote files already exist",
				Code:      "remote_files_exist",
				Conflicts: conflicts,
			})
		}
		return
	}
	batch, err := runtime.fileTransfers.CreateBatch(r.Context(), filetransfer.CreateBatchRequest{
		ServerID:  serverID,
		Direction: filetransfer.DirectionUpload,
		Source:    filetransfer.SourceUI,
		Items:     requests,
	})
	if err != nil {
		cleanupTempPaths(tempPaths)
		writeInternalError(w)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, serverID, "file_transfer.batch.upload.started", map[string]any{
		"batch_id":   batch.ID,
		"items":      len(batch.Items),
		"remote_dir": remoteDir,
		"size_bytes": batch.SizeBytes,
		"overwrite":  overwrite,
	})
	go s.runTransferBatch(runtime, batch.ID, overwrite)
	writeJSON(w, http.StatusAccepted, batch)
}

func (s fileTransferHandlers) startDownload(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request startDownloadRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	remotePath, err := normalizeRemoteFilePath(request.RemotePath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.ServerID < 1 {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	tempPath, err := s.reserveDownloadTempFile()
	if err != nil {
		writeInternalError(w)
		return
	}
	fileName := safeFileName(path.Base(remotePath))
	record, err := runtime.fileTransfers.Create(r.Context(), filetransfer.CreateRequest{
		ServerID:   request.ServerID,
		Direction:  filetransfer.DirectionDownload,
		Source:     filetransfer.SourceUI,
		RemotePath: remotePath,
		FileName:   fileName,
		TempPath:   tempPath,
	})
	if err != nil {
		_ = os.Remove(tempPath)
		writeInternalError(w)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, request.ServerID, "file_transfer.download.started", map[string]any{
		"transfer_id": record.ID,
		"remote_path": remotePath,
		"file_name":   fileName,
	})
	go s.runDownload(runtime, record.ID)
	writeJSON(w, http.StatusAccepted, record)
}

func (s fileTransferHandlers) startDownloadBatch(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request startDownloadBatchRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if request.ServerID < 1 {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	if len(request.RemotePaths) == 0 {
		writeError(w, http.StatusBadRequest, "remote_paths is required")
		return
	}
	if len(request.RemotePaths) > 100 {
		writeError(w, http.StatusBadRequest, "cannot download more than 100 files at once")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	server, privateKey, err := s.serverSSHMaterialFromRuntime(ctx, runtime, request.ServerID)
	if err != nil {
		handleServerSSHMaterialError(w, err)
		return
	}
	target := s.executionTarget(server, privateKey)
	items := make([]filetransfer.CreateRequest, 0, len(request.RemotePaths))
	tempPaths := []string{}
	seenRemotePaths := map[string]bool{}
	var totalSize int64
	for _, raw := range request.RemotePaths {
		remotePath, err := normalizeRemoteFilePath(raw)
		if err != nil {
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if seenRemotePaths[remotePath] {
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusBadRequest, "download queue contains duplicate remote paths")
			return
		}
		seenRemotePaths[remotePath] = true
		status, err := execution.StatRemotePath(ctx, target, remotePath)
		if err != nil {
			cleanupTempPaths(tempPaths)
			if writeUnknownHostKeyError(w, err) {
				return
			}
			writeError(w, http.StatusBadGateway, sshConnectionFailureMessage(err))
			return
		}
		if !status.Exists || status.Type != "file" {
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusBadRequest, "remote path must be an existing regular file")
			return
		}
		totalSize += status.Size
		if totalSize > maxFileTransferBatchBytes {
			cleanupTempPaths(tempPaths)
			writeError(w, http.StatusRequestEntityTooLarge, "download batch cannot exceed 1 GiB total size")
			return
		}
		tempPath, err := s.reserveDownloadTempFile()
		if err != nil {
			cleanupTempPaths(tempPaths)
			writeInternalError(w)
			return
		}
		tempPaths = append(tempPaths, tempPath)
		fileName := safeFileName(path.Base(remotePath))
		items = append(items, filetransfer.CreateRequest{
			RemotePath: remotePath,
			FileName:   fileName,
			SizeBytes:  status.Size,
			TempPath:   tempPath,
		})
	}
	archiveName := ""
	if strings.TrimSpace(request.ArchiveName) != "" {
		archiveName = safeFileName(request.ArchiveName)
	}
	if archiveName == "" && len(items) > 1 {
		archiveName = fmt.Sprintf("aipermission-download-%s.zip", time.Now().UTC().Format("20060102-150405"))
	}
	batch, err := runtime.fileTransfers.CreateBatch(r.Context(), filetransfer.CreateBatchRequest{
		ServerID:    request.ServerID,
		Direction:   filetransfer.DirectionDownload,
		Source:      filetransfer.SourceUI,
		ArchiveName: archiveName,
		Items:       items,
	})
	if err != nil {
		cleanupTempPaths(tempPaths)
		writeInternalError(w)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, request.ServerID, "file_transfer.batch.download.started", map[string]any{
		"batch_id":   batch.ID,
		"items":      len(batch.Items),
		"size_bytes": batch.SizeBytes,
	})
	go s.runTransferBatch(runtime, batch.ID, false)
	writeJSON(w, http.StatusAccepted, batch)
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
	server, privateKey, err := s.serverSSHMaterialFromRuntime(ctx, runtime, item.ServerID)
	if err != nil {
		s.failFileTransfer(runtime, transferID, err)
		return
	}
	result, err := execution.UploadFile(ctx, s.executionTarget(server, privateKey), item.TempPath, item.RemotePath, overwrite, s.transferProgress(runtime, transferID))
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
	s.writeAudit(context.Background(), runtime, "user", nil, item.ServerID, "file_transfer.upload.completed", map[string]any{
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
	server, privateKey, err := s.serverSSHMaterialFromRuntime(ctx, runtime, item.ServerID)
	if err != nil {
		s.failFileTransfer(runtime, transferID, err)
		return
	}
	result, err := execution.DownloadFile(ctx, s.executionTarget(server, privateKey), item.RemotePath, item.TempPath, s.transferProgress(runtime, transferID))
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
	s.writeAudit(context.Background(), runtime, "user", nil, item.ServerID, "file_transfer.download.completed", map[string]any{
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

	if ok, err := runtime.fileTransfers.MarkBatchRunning(ctx, batchID); err != nil {
		log.Printf("mark file transfer batch running failed batch=%d error=%v", batchID, err)
		return
	} else if !ok {
		return
	}
	batch, err := runtime.fileTransfers.GetBatch(ctx, batchID)
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
	if batch.Direction == filetransfer.DirectionDownload && batch.FailedItems == 0 && batch.CanceledItems == 0 {
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
		s.writeAudit(context.Background(), runtime, "user", nil, batch.ServerID, "file_transfer.batch.completed", map[string]any{
			"batch_id":        batchID,
			"direction":       batch.Direction,
			"items":           len(batch.Items),
			"completed_items": batch.CompletedItems,
			"failed_items":    batch.FailedItems,
			"canceled_items":  batch.CanceledItems,
		})
	}
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
	server, privateKey, err := s.serverSSHMaterialFromRuntime(itemCtx, runtime, item.ServerID)
	if err != nil {
		s.failFileTransfer(runtime, transferID, err)
		return
	}
	options := execution.TransferOptions{
		Progress: s.transferProgress(runtime, transferID),
		Wait:     control.Wait,
	}
	var result execution.TransferResult
	if item.Direction == filetransfer.DirectionUpload {
		defer s.removeTransferTemp(runtime, transferID)
		result, err = execution.UploadFileWithOptions(itemCtx, s.executionTarget(server, privateKey), item.TempPath, item.RemotePath, overwrite, options)
	} else {
		result, err = execution.DownloadFileWithOptions(itemCtx, s.executionTarget(server, privateKey), item.RemotePath, item.TempPath, options)
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
	s.writeAudit(context.Background(), runtime, "user", nil, item.ServerID, "file_transfer.completed", map[string]any{
		"transfer_id":     transferID,
		"batch_id":        item.BatchID,
		"direction":       item.Direction,
		"remote_path":     item.RemotePath,
		"bytes":           result.Bytes,
		"checksum_sha256": result.ChecksumSHA256,
		"duration_ms":     result.DurationMS,
	})
}

func (s fileTransferHandlers) transferProgress(runtime *databaseRuntime, transferID int64) execution.TransferProgress {
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

func (s fileTransferHandlers) checkUploadOverwrite(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, serverID int64, remotePath string, overwrite bool, tempPath string) bool {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	server, privateKey, err := s.serverSSHMaterialFromRuntime(ctx, runtime, serverID)
	if err != nil {
		_ = os.Remove(tempPath)
		handleServerSSHMaterialError(w, err)
		return false
	}
	status, err := execution.StatRemotePath(ctx, s.executionTarget(server, privateKey), remotePath)
	if err != nil {
		_ = os.Remove(tempPath)
		if writeUnknownHostKeyError(w, err) {
			return false
		}
		writeError(w, http.StatusBadGateway, sshConnectionFailureMessage(err))
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

func (s fileTransferHandlers) checkUploadBatchOverwrite(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, serverID int64, requests []filetransfer.CreateRequest, overwrite bool, tempPaths []string) ([]remoteFileConflict, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	server, privateKey, err := s.serverSSHMaterialFromRuntime(ctx, runtime, serverID)
	if err != nil {
		cleanupTempPaths(tempPaths)
		handleServerSSHMaterialError(w, err)
		return nil, false
	}
	target := s.executionTarget(server, privateKey)
	var conflicts []remoteFileConflict
	for _, item := range requests {
		status, err := execution.StatRemotePath(ctx, target, item.RemotePath)
		if err != nil {
			cleanupTempPaths(tempPaths)
			if writeUnknownHostKeyError(w, err) {
				return nil, false
			}
			writeError(w, http.StatusBadGateway, sshConnectionFailureMessage(err))
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
		s.writeAudit(context.Background(), runtime, "user", nil, item.ServerID, "file_transfer.failed", map[string]any{
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
		s.writeAudit(context.Background(), runtime, "user", nil, item.ServerID, "file_transfer.canceled", map[string]any{
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

func (s fileTransferHandlers) stageUploadFile(reader io.Reader) (string, int64, error) {
	root, err := s.ensureFileTransferTempRoot()
	if err != nil {
		return "", 0, err
	}
	temp, err := os.CreateTemp(root, "upload-*")
	if err != nil {
		return "", 0, fmt.Errorf("create temporary upload file: %w", err)
	}
	tempPath := temp.Name()
	defer temp.Close()
	size, err := io.Copy(temp, reader)
	if err != nil {
		_ = os.Remove(tempPath)
		return "", 0, fmt.Errorf("stage upload file: %w", err)
	}
	return tempPath, size, nil
}

func (s fileTransferHandlers) reserveDownloadTempFile() (string, error) {
	root, err := s.ensureFileTransferTempRoot()
	if err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(root, "download-*")
	if err != nil {
		return "", fmt.Errorf("create temporary download file: %w", err)
	}
	path := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close temporary download file: %w", err)
	}
	return path, nil
}

func (s fileTransferHandlers) removeTransferTemp(runtime *databaseRuntime, transferID int64) {
	item, err := runtime.fileTransfers.Get(context.Background(), transferID)
	if err != nil || item.TempPath == "" || !s.tempPathAllowed(item.TempPath) {
		return
	}
	_ = os.Remove(item.TempPath)
}

func (s fileTransferHandlers) cleanupBatchTemps(runtime *databaseRuntime, batchID int64) {
	batch, err := runtime.fileTransfers.GetBatch(context.Background(), batchID)
	if err != nil {
		return
	}
	if batch.ArchivePath != "" && s.tempPathAllowed(batch.ArchivePath) {
		_ = os.Remove(batch.ArchivePath)
	}
	for _, item := range batch.Items {
		if item.TempPath != "" && s.tempPathAllowed(item.TempPath) {
			_ = os.Remove(item.TempPath)
		}
	}
}

func (s fileTransferHandlers) scheduleBatchItemTempCleanup(batch filetransfer.BatchRecord) {
	for _, item := range batch.Items {
		if item.TempPath != "" {
			s.scheduleTransferTempCleanup(item.TempPath)
		}
	}
}

func (s fileTransferHandlers) createDownloadArchive(batch filetransfer.BatchRecord) (string, error) {
	root, err := s.ensureFileTransferTempRoot()
	if err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(root, "archive-*.zip")
	if err != nil {
		return "", fmt.Errorf("create temporary download archive: %w", err)
	}
	archivePath := temp.Name()
	zipWriter := zip.NewWriter(temp)
	usedNames := map[string]int{}
	for _, item := range batch.Items {
		if item.Status != filetransfer.StatusCompleted {
			continue
		}
		if item.TempPath == "" || !s.tempPathAllowed(item.TempPath) {
			_ = zipWriter.Close()
			_ = temp.Close()
			_ = os.Remove(archivePath)
			return "", fmt.Errorf("download file is no longer available")
		}
		entryName := uniqueArchiveEntryName(item.FileName, item.RemotePath, usedNames)
		if err := addFileToZip(zipWriter, item.TempPath, entryName); err != nil {
			_ = zipWriter.Close()
			_ = temp.Close()
			_ = os.Remove(archivePath)
			return "", err
		}
	}
	if err := zipWriter.Close(); err != nil {
		_ = temp.Close()
		_ = os.Remove(archivePath)
		return "", fmt.Errorf("close download archive: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(archivePath)
		return "", fmt.Errorf("close temporary download archive: %w", err)
	}
	return archivePath, nil
}

func uniqueArchiveEntryName(name string, remotePath string, used map[string]int) string {
	base := safeFileName(name)
	if base == "aipermission-file" && strings.TrimSpace(remotePath) != "" {
		base = safeFileName(path.Base(remotePath))
	}
	if base == "" {
		base = "aipermission-file"
	}
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = "file"
	}
	return fmt.Sprintf("%s-%d%s", stem, count+1, ext)
}

func addFileToZip(zipWriter *zip.Writer, filePath string, name string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open downloaded file for archive: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat downloaded file for archive: %w", err)
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf("create archive header: %w", err)
	}
	header.Name = safeFileName(name)
	if header.Name == "" {
		header.Name = safeFileName(filepath.Base(filePath))
	}
	header.Method = zip.Deflate
	if shouldStoreArchiveEntry(header.Name) {
		header.Method = zip.Store
	}
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create archive entry: %w", err)
	}
	if _, err := io.Copy(writer, file); err != nil {
		return fmt.Errorf("write archive entry: %w", err)
	}
	return nil
}

func shouldStoreArchiveEntry(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip", ".gz", ".tgz", ".bz2", ".xz", ".7z", ".rar":
		return true
	default:
		return false
	}
}

func setDownloadHeaders(w http.ResponseWriter, fileName string) {
	contentType := mime.TypeByExtension(filepath.Ext(fileName))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileName}))
}

func (s fileTransferHandlers) ensureFileTransferTempRoot() (string, error) {
	root := s.fileTransferTempRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create file transfer temp directory: %w", err)
	}
	return root, nil
}

func (s fileTransferHandlers) fileTransferTempRoot() string {
	return filepath.Join(filepath.Dir(s.config.DataPath), "file-transfers")
}

func cleanupTempPaths(paths []string) {
	for _, path := range paths {
		if path != "" {
			_ = os.Remove(path)
		}
	}
}

func joinRemoteFilePath(remoteDir string, fileName string) string {
	cleanName := strings.TrimLeft(safeFileName(fileName), "/")
	if cleanName == "" {
		cleanName = "file"
	}
	if remoteDir == "/" {
		return "/" + cleanName
	}
	return strings.TrimRight(remoteDir, "/") + "/" + cleanName
}

func transferSpeedAndETA(transferred int64, total int64, elapsed time.Duration) (int64, int64) {
	if transferred <= 0 || elapsed <= 0 {
		return 0, -1
	}
	bytesPerSecond := int64(float64(transferred) / elapsed.Seconds())
	if bytesPerSecond <= 0 || total <= 0 || transferred >= total {
		if transferred >= total && total > 0 {
			return bytesPerSecond, 0
		}
		return bytesPerSecond, -1
	}
	remaining := total - transferred
	return bytesPerSecond, remaining / bytesPerSecond
}

func (s fileTransferHandlers) tempPathAllowed(value string) bool {
	root, err := filepath.Abs(s.fileTransferTempRoot())
	if err != nil {
		return false
	}
	target, err := filepath.Abs(value)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."
}

func (s fileTransferHandlers) scheduleTransferTempCleanup(path string) {
	if path == "" || !s.tempPathAllowed(path) {
		return
	}
	time.AfterFunc(fileTransferTempTTL, func() {
		_ = os.Remove(path)
	})
}

func (s fileTransferHandlers) serverSSHMaterialFromRuntime(ctx context.Context, runtime *databaseRuntime, serverID int64) (servers.Server, sshkeys.PrivateKey, error) {
	server, privateKey, err := s.serverSSHMaterialForRuntime(runtime)(ctx, serverID)
	if err != nil {
		return servers.Server{}, sshkeys.PrivateKey{}, err
	}
	return server, privateKey, nil
}

func parseFormInt64(w http.ResponseWriter, r *http.Request, field string) (int64, bool) {
	value := strings.TrimSpace(r.FormValue(field))
	if value == "" {
		writeError(w, http.StatusBadRequest, field+" is required")
		return 0, false
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid "+field)
		return 0, false
	}
	return id, true
}

func parseFormBool(r *http.Request, field string) bool {
	switch strings.ToLower(strings.TrimSpace(r.FormValue(field))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func normalizeRemoteFilePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("remote_path is required")
	}
	if len([]rune(value)) > 4096 {
		return "", fmt.Errorf("remote_path must be 4096 characters or fewer")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("remote_path cannot contain control characters")
		}
	}
	if !path.IsAbs(value) {
		return "", fmt.Errorf("remote_path must be an absolute path")
	}
	cleaned := path.Clean(value)
	if cleaned == "/" || path.Base(cleaned) == "." {
		return "", fmt.Errorf("remote_path must point to a file")
	}
	return cleaned, nil
}

func normalizeRemoteDirectoryPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "/"
	}
	if len([]rune(value)) > 4096 {
		return "", fmt.Errorf("path must be 4096 characters or fewer")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("path cannot contain control characters")
		}
	}
	if !path.IsAbs(value) {
		return "", fmt.Errorf("path must be an absolute path")
	}
	return path.Clean(value), nil
}

func safeFileName(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = path.Base(value)
	value = strings.Trim(value, ". ")
	if value == "" || value == "/" || value == "." {
		return "aipermission-file"
	}
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			builder.WriteRune('_')
			continue
		}
		builder.WriteRune(r)
	}
	result := strings.TrimSpace(builder.String())
	if result == "" {
		return "aipermission-file"
	}
	if len([]rune(result)) > 160 {
		return string([]rune(result)[:160])
	}
	return result
}

func validFileTransferStatus(status string) bool {
	switch status {
	case filetransfer.StatusPending, filetransfer.StatusRunning, filetransfer.StatusPaused, filetransfer.StatusCompleted, filetransfer.StatusFailed, filetransfer.StatusCanceled:
		return true
	default:
		return false
	}
}
