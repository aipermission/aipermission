package api

import (
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
	fileTransferTimeout        = 2 * time.Hour
	fileTransferTempTTL        = 30 * time.Minute
)

type startDownloadRequest struct {
	ServerID   int64  `json:"server_id"`
	RemotePath string `json:"remote_path"`
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
	if item.Status != filetransfer.StatusPending && item.Status != filetransfer.StatusRunning {
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
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileName}))
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

func (s fileTransferHandlers) transferProgress(runtime *databaseRuntime, transferID int64) execution.TransferProgress {
	var lastWrite time.Time
	return func(transferred int64, total int64) {
		now := time.Now()
		if transferred != total && now.Sub(lastWrite) < 250*time.Millisecond {
			return
		}
		lastWrite = now
		if err := runtime.fileTransfers.UpdateProgress(context.Background(), transferID, transferred, total); err != nil {
			log.Printf("update file transfer progress failed transfer=%d error=%v", transferID, err)
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
	case filetransfer.StatusPending, filetransfer.StatusRunning, filetransfer.StatusCompleted, filetransfer.StatusFailed, filetransfer.StatusCanceled:
		return true
	default:
		return false
	}
}
