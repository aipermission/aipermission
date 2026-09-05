package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

const (
	maxFileTransferObjectBytes       = 512 << 20
	maxFileTransferBatchBytes        = 1 << 30
	maxFileTransferBatchItems        = 100
	maxFileTransferMultipartOverhead = 16 << 20
	fileTransferTimeout              = 2 * time.Hour
	fileTransferBatchTimeout         = 6 * time.Hour
	fileTransferTempTTL              = 30 * time.Minute
)

func formatFileTransferLimit(size int64) string {
	const (
		mib = int64(1 << 20)
		gib = int64(1 << 30)
	)
	switch {
	case size >= gib && size%gib == 0:
		return fmt.Sprintf("%d GiB", size/gib)
	case size >= mib && size%mib == 0:
		return fmt.Sprintf("%d MiB", size/mib)
	default:
		return fmt.Sprintf("%d bytes", size)
	}
}

type startDownloadRequest struct {
	RuntimeID  int64  `json:"runtime_id"`
	RemotePath string `json:"remote_path"`
}

type startDownloadBatchRequest struct {
	RuntimeID   int64    `json:"runtime_id"`
	RemotePaths []string `json:"remote_paths"`
	ArchiveName string   `json:"archive_name"`
}

type updateFileTransferBatchQueueRequest struct {
	ItemIDs []int64 `json:"item_ids"`
}

type approveFileTransferBatchRequest struct {
	ItemIDs []int64 `json:"item_ids"`
	Note    string  `json:"note"`
}

type declineFileTransferBatchRequest struct {
	Note string `json:"note"`
}

type browseRemoteFilesRequest struct {
	RuntimeID int64  `json:"runtime_id"`
	Path      string `json:"path"`
	Cursor    string `json:"cursor"`
}

type browseRemoteFilesResponse struct {
	Path       string                         `json:"path"`
	Parent     string                         `json:"parent"`
	Entries    []connectorapi.RemoteFileEntry `json:"entries"`
	NextCursor string                         `json:"next_cursor,omitempty"`
	HasMore    bool                           `json:"has_more"`
}

type expandRemoteFilesRequest struct {
	RuntimeID int64  `json:"runtime_id"`
	Path      string `json:"path"`
}

type expandRemoteFilesResponse struct {
	Path       string                         `json:"path"`
	Entries    []connectorapi.RemoteFileEntry `json:"entries"`
	TotalBytes int64                          `json:"total_bytes"`
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

type fileTransferStartError struct {
	Status  int
	Message string
}

func (err *fileTransferStartError) Error() string {
	return err.Message
}

func newFileTransferStartError(status int, message string) error {
	return &fileTransferStartError{Status: status, Message: message}
}

type fileTransferConnectorError struct {
	Adapter connectorapi.FileTransferAdapter
	Err     error
}

func (err *fileTransferConnectorError) Error() string {
	if err == nil || err.Err == nil {
		return ""
	}
	return err.Err.Error()
}

func newFileTransferConnectorError(adapter connectorapi.FileTransferAdapter, err error) error {
	return &fileTransferConnectorError{Adapter: adapter, Err: err}
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
	if rawRuntimeID := strings.TrimSpace(r.URL.Query().Get("runtime_id")); rawRuntimeID != "" {
		id, ok := parseInt64Query(w, rawRuntimeID, "runtime_id")
		if !ok {
			return
		}
		filter.RuntimeID = id
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
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.RuntimeID < 1 {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	remotePath, err := s.normalizeTransferPath(r.Context(), runtime, request.RuntimeID, request.Path, true)
	if err != nil {
		writeTransferPathError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	adapter, err := s.fileTransferAdapter(ctx, runtime, request.RuntimeID)
	if err != nil {
		handleConnectorTargetRuntimeError(w, err)
		return
	}
	page := connectorapi.RemoteFilePage{}
	if paginated, ok := adapter.(connectorapi.PaginatedFileTransferAdapter); ok {
		page, err = paginated.BrowseRemoteFilesPage(ctx, s.Server, runtime, request.RuntimeID, remotePath, strings.TrimSpace(request.Cursor))
	} else if strings.TrimSpace(request.Cursor) != "" {
		writeError(w, http.StatusBadRequest, "this connector does not support paginated file browsing")
		return
	} else {
		page.Entries, err = adapter.BrowseRemoteFiles(ctx, s.Server, runtime, request.RuntimeID, remotePath)
	}
	if err != nil {
		s.writeCredentialSafeConnectorError(w, ctx, runtime, request.RuntimeID, adapter, http.StatusBadGateway, "remote file browse failed", err)
		return
	}
	parent := transferParent(adapter, remotePath)
	response := browseRemoteFilesResponse{
		Path:       remotePath,
		Parent:     parent,
		Entries:    page.Entries,
		NextCursor: page.NextCursor,
		HasMore:    page.HasMore,
	}
	boundary, boundaryErr := connectorCredentialBoundaryForRuntimeID(ctx, runtime, request.RuntimeID)
	if boundaryErr != nil || connectorValueContainsCredential(boundary, response) {
		writeError(w, http.StatusBadGateway, "remote file browse violated the credential boundary")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s fileTransferHandlers) expandRemoteFiles(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request expandRemoteFilesRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.RuntimeID < 1 {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	remotePath, err := s.normalizeTransferPath(r.Context(), runtime, request.RuntimeID, request.Path, true)
	if err != nil {
		writeTransferPathError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	baseAdapter, err := s.fileTransferAdapter(ctx, runtime, request.RuntimeID)
	if err != nil {
		handleConnectorTargetRuntimeError(w, err)
		return
	}
	adapter, ok := baseAdapter.(connectorapi.RecursiveFileTransferAdapter)
	if !ok {
		writeError(w, http.StatusConflict, "this connector does not support recursive file selection")
		return
	}
	entries, err := adapter.ListRecursiveFiles(ctx, s.Server, runtime, request.RuntimeID, remotePath, maxFileTransferBatchItems, maxFileTransferObjectBytes, maxFileTransferBatchBytes)
	if err != nil {
		if errors.Is(err, connectorapi.ErrRemotePathNotFound) {
			s.writeCredentialSafeConnectorError(w, ctx, runtime, request.RuntimeID, baseAdapter, http.StatusNotFound, "remote path was not found", err)
			return
		}
		if errors.Is(err, connectorapi.ErrTransferLimit) {
			s.writeCredentialSafeConnectorError(w, ctx, runtime, request.RuntimeID, baseAdapter, http.StatusRequestEntityTooLarge, "recursive file selection exceeded its limit", err)
			return
		}
		s.writeCredentialSafeConnectorError(w, ctx, runtime, request.RuntimeID, baseAdapter, http.StatusBadGateway, "recursive file selection failed", err)
		return
	}
	var totalBytes int64
	for _, entry := range entries {
		totalBytes += entry.Size
	}
	response := expandRemoteFilesResponse{Path: remotePath, Entries: entries, TotalBytes: totalBytes}
	boundary, boundaryErr := connectorCredentialBoundaryForRuntimeID(ctx, runtime, request.RuntimeID)
	if boundaryErr != nil || connectorValueContainsCredential(boundary, response) {
		writeError(w, http.StatusBadGateway, "recursive file selection violated the credential boundary")
		return
	}
	writeJSON(w, http.StatusOK, response)
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
	}
	updated, err := runtime.fileTransfers.Get(r.Context(), id)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
