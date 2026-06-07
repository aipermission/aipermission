package api

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/execution"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

const (
	defaultMCPFileTransferLimit = 20
	maxMCPFileTransferLimit     = 100
)

type mcpFileTransferItem struct {
	ID               int64  `json:"id"`
	BatchID          int64  `json:"batch_id,omitempty"`
	QueueIndex       int    `json:"queue_index,omitempty"`
	ServerID         int64  `json:"server_id"`
	ServerName       string `json:"server_name,omitempty"`
	Direction        string `json:"direction"`
	Source           string `json:"source"`
	Status           string `json:"status"`
	RemotePath       string `json:"remote_path"`
	FileName         string `json:"file_name"`
	SizeBytes        int64  `json:"size_bytes"`
	TransferredBytes int64  `json:"transferred_bytes"`
	BytesPerSecond   int64  `json:"bytes_per_second"`
	ETASeconds       int64  `json:"eta_seconds"`
	ChecksumSHA256   string `json:"checksum_sha256,omitempty"`
	Error            string `json:"error,omitempty"`
	CreatedAt        string `json:"created_at"`
	StartedAt        string `json:"started_at,omitempty"`
	CompletedAt      string `json:"completed_at,omitempty"`
	UpdatedAt        string `json:"updated_at"`
}

type mcpFileTransferBatchItem struct {
	ID               int64                 `json:"id"`
	ServerID         int64                 `json:"server_id"`
	ServerName       string                `json:"server_name,omitempty"`
	Direction        string                `json:"direction"`
	Source           string                `json:"source"`
	Status           string                `json:"status"`
	ArchiveName      string                `json:"archive_name,omitempty"`
	ApprovalNote     string                `json:"approval_note,omitempty"`
	Overwrite        bool                  `json:"overwrite,omitempty"`
	TotalItems       int                   `json:"total_items"`
	CompletedItems   int                   `json:"completed_items"`
	FailedItems      int                   `json:"failed_items"`
	CanceledItems    int                   `json:"canceled_items"`
	SizeBytes        int64                 `json:"size_bytes"`
	TransferredBytes int64                 `json:"transferred_bytes"`
	BytesPerSecond   int64                 `json:"bytes_per_second"`
	ETASeconds       int64                 `json:"eta_seconds"`
	Error            string                `json:"error,omitempty"`
	CreatedAt        string                `json:"created_at"`
	StartedAt        string                `json:"started_at,omitempty"`
	CompletedAt      string                `json:"completed_at,omitempty"`
	UpdatedAt        string                `json:"updated_at"`
	Items            []mcpFileTransferItem `json:"items,omitempty"`
}

type mcpFileTransferPage struct {
	Items  []mcpFileTransferItem `json:"items"`
	Total  int                   `json:"total"`
	Limit  int                   `json:"limit"`
	Offset int                   `json:"offset"`
}

type mcpFileTransferBatchPage struct {
	Items  []mcpFileTransferBatchItem `json:"items"`
	Total  int                        `json:"total"`
	Limit  int                        `json:"limit"`
	Offset int                        `json:"offset"`
}

type mcpFileTransferActionResponse struct {
	Status            string                    `json:"status"`
	ServerID          int64                     `json:"server_id,omitempty"`
	ServerName        string                    `json:"server_name,omitempty"`
	Batch             *mcpFileTransferBatchItem `json:"batch,omitempty"`
	Error             string                    `json:"error,omitempty"`
	RetryAfterSeconds int                       `json:"retry_after_seconds,omitempty"`
	AssistantHint     string                    `json:"assistant_hint,omitempty"`
}

type mcpStartFileDownloadRequest struct {
	ServerID    int64    `json:"server_id"`
	RemotePaths []string `json:"remote_paths"`
	ArchiveName string   `json:"archive_name,omitempty"`
}

type mcpTransferPermissionResult struct {
	ServerName string
	Rule       string
}

func (s mcpHandlers) mcpListFileTransfers(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	limit, offset, ok := parseMCPTransferListPagination(w, r)
	if !ok {
		return
	}
	filter := filetransfer.ListFilter{
		Direction: strings.TrimSpace(r.URL.Query().Get("direction")),
		Status:    strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:     limit,
		Offset:    offset,
	}
	if !validMCPTransferFilter(w, filter.Direction, filter.Status) {
		return
	}
	if rawServerID := strings.TrimSpace(r.URL.Query().Get("server_id")); rawServerID != "" {
		serverID, ok := parseInt64Query(w, rawServerID, "server_id")
		if !ok {
			return
		}
		canSee, err := s.mcpCanSeeServer(r.Context(), auth.runtime, auth.TokenID, serverID)
		if err != nil {
			writeInternalError(w)
			return
		}
		if !canSee {
			writeJSON(w, http.StatusOK, mcpFileTransferPage{Items: []mcpFileTransferItem{}, Limit: limit, Offset: offset})
			return
		}
		filter.ServerID = serverID
	} else {
		serverIDs, err := s.mcpAllowedServerIDs(r.Context(), auth.runtime, auth.TokenID)
		if err != nil {
			writeInternalError(w)
			return
		}
		if len(serverIDs) == 0 {
			writeJSON(w, http.StatusOK, mcpFileTransferPage{Items: []mcpFileTransferItem{}, Limit: limit, Offset: offset})
			return
		}
		filter.ServerIDs = serverIDs
	}

	items, total, err := auth.runtime.fileTransfers.List(r.Context(), filter)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, mcpFileTransferPage{
		Items:  sanitizeMCPFileTransferItems(items),
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (s mcpHandlers) mcpGetFileTransfer(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	item, err := auth.runtime.fileTransfers.Get(r.Context(), id)
	if errors.Is(err, filetransfer.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file transfer not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	canSee, err := s.mcpCanSeeServer(r.Context(), auth.runtime, auth.TokenID, item.ServerID)
	if err != nil {
		writeInternalError(w)
		return
	}
	if !canSee {
		writeError(w, http.StatusNotFound, "file transfer not found")
		return
	}
	writeJSON(w, http.StatusOK, sanitizeMCPFileTransferItem(item))
}

func (s mcpHandlers) mcpListFileTransferBatches(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	limit, offset, ok := parseMCPTransferListPagination(w, r)
	if !ok {
		return
	}
	filter := filetransfer.BatchListFilter{
		Direction: strings.TrimSpace(r.URL.Query().Get("direction")),
		Status:    strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:     limit,
		Offset:    offset,
	}
	if !validMCPTransferFilter(w, filter.Direction, filter.Status) {
		return
	}
	if rawServerID := strings.TrimSpace(r.URL.Query().Get("server_id")); rawServerID != "" {
		serverID, ok := parseInt64Query(w, rawServerID, "server_id")
		if !ok {
			return
		}
		canSee, err := s.mcpCanSeeServer(r.Context(), auth.runtime, auth.TokenID, serverID)
		if err != nil {
			writeInternalError(w)
			return
		}
		if !canSee {
			writeJSON(w, http.StatusOK, mcpFileTransferBatchPage{Items: []mcpFileTransferBatchItem{}, Limit: limit, Offset: offset})
			return
		}
		filter.ServerID = serverID
	} else {
		serverIDs, err := s.mcpAllowedServerIDs(r.Context(), auth.runtime, auth.TokenID)
		if err != nil {
			writeInternalError(w)
			return
		}
		if len(serverIDs) == 0 {
			writeJSON(w, http.StatusOK, mcpFileTransferBatchPage{Items: []mcpFileTransferBatchItem{}, Limit: limit, Offset: offset})
			return
		}
		filter.ServerIDs = serverIDs
	}

	items, total, err := auth.runtime.fileTransfers.ListBatches(r.Context(), filter)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, mcpFileTransferBatchPage{
		Items:  sanitizeMCPFileTransferBatches(items, false),
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (s mcpHandlers) mcpGetFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	batch, err := auth.runtime.fileTransfers.GetBatch(r.Context(), id)
	if errors.Is(err, filetransfer.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file transfer batch not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	canSee, err := s.mcpCanSeeServer(r.Context(), auth.runtime, auth.TokenID, batch.ServerID)
	if err != nil {
		writeInternalError(w)
		return
	}
	if !canSee {
		writeError(w, http.StatusNotFound, "file transfer batch not found")
		return
	}
	writeJSON(w, http.StatusOK, sanitizeMCPFileTransferBatch(batch, true))
}

func (s mcpHandlers) mcpBrowseRemoteFiles(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	if s.rejectStoppedMCP(w, auth.runtime) {
		return
	}
	var request browseRemoteFilesRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	serverName, ok := s.requireMCPTransferControl(w, r.Context(), auth.runtime, auth.TokenID, request.ServerID)
	if !ok {
		return
	}
	remotePath, err := normalizeRemoteDirectoryPath(request.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	server, privateKey, err := fileTransferHandlers{s.Server}.serverSSHMaterialFromRuntime(ctx, auth.runtime, request.ServerID)
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
	parent := pathDirForMCPBrowse(remotePath)
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id":   request.ServerID,
		"server_name": serverName,
		"path":        remotePath,
		"parent":      parent,
		"entries":     entries,
	})
}

func (s mcpHandlers) mcpStartFileDownload(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	if s.rejectStoppedMCP(w, auth.runtime) {
		return
	}
	var request mcpStartFileDownloadRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	permission, ok := s.mcpTransferStartPermission(w, r.Context(), auth.runtime, auth.TokenID, request.ServerID)
	if !ok {
		return
	}
	initialStatus := filetransfer.StatusPending
	if permission.Rule == tokens.RuleApprovalRequired {
		initialStatus = filetransfer.StatusPendingApproval
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	transfers := fileTransferHandlers{s.Server}
	batch, err := transfers.createDownloadBatch(ctx, auth.runtime, request.ServerID, request.RemotePaths, request.ArchiveName, filetransfer.SourceMCP, initialStatus)
	if err != nil {
		if transfers.writeFileTransferStartError(w, err) {
			return
		}
		writeInternalError(w)
		return
	}
	action := "mcp.file_transfer.batch.download.started"
	if initialStatus == filetransfer.StatusPendingApproval {
		action = "mcp.file_transfer.batch.download.approval_requested"
	}
	s.writeAudit(r.Context(), auth.runtime, "mcp", int64Ptr(auth.TokenID), request.ServerID, action, map[string]any{
		"batch_id":   batch.ID,
		"items":      len(batch.Items),
		"size_bytes": batch.SizeBytes,
	})
	if initialStatus != filetransfer.StatusPendingApproval {
		go transfers.runTransferBatch(auth.runtime, batch.ID, false)
	}
	sanitized := sanitizeMCPFileTransferBatch(batch, true)
	hint := "Poll get_file_transfer_batch for progress. The completed archive or file is staged locally for the human operator to save from the AIPermission UI."
	retryAfter := 3
	if sanitized.Status == filetransfer.StatusPendingApproval {
		hint = "This download queue is waiting for local approval in AIPermission Transfer Center. Poll get_file_transfer_batch; approved files will transfer after the user decides."
		retryAfter = 5
	}
	writeJSON(w, http.StatusAccepted, mcpFileTransferActionResponse{
		Status:            sanitized.Status,
		ServerID:          request.ServerID,
		ServerName:        permission.ServerName,
		Batch:             &sanitized,
		RetryAfterSeconds: retryAfter,
		AssistantHint:     hint,
	})
}

func (s mcpHandlers) mcpStartFileUpload(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	if s.rejectStoppedMCP(w, auth.runtime) {
		return
	}
	var serverID int64
	var serverName string
	initialStatus := filetransfer.StatusPending
	transfers := fileTransferHandlers{s.Server}
	batch, _, overwrite, ok := transfers.createUploadBatchFromMultipart(w, r, auth.runtime, filetransfer.SourceMCP, &initialStatus, func(nextServerID int64) bool {
		permission, allowed := s.mcpTransferStartPermission(w, r.Context(), auth.runtime, auth.TokenID, nextServerID)
		if !allowed {
			return false
		}
		serverID = nextServerID
		serverName = permission.ServerName
		if permission.Rule == tokens.RuleApprovalRequired {
			initialStatus = filetransfer.StatusPendingApproval
		}
		return true
	})
	if !ok {
		return
	}
	action := "mcp.file_transfer.batch.upload.started"
	if batch.Status == filetransfer.StatusPendingApproval {
		action = "mcp.file_transfer.batch.upload.approval_requested"
	}
	s.writeAudit(r.Context(), auth.runtime, "mcp", int64Ptr(auth.TokenID), serverID, action, map[string]any{
		"batch_id":   batch.ID,
		"items":      len(batch.Items),
		"size_bytes": batch.SizeBytes,
		"overwrite":  overwrite,
	})
	if batch.Status != filetransfer.StatusPendingApproval {
		go transfers.runTransferBatch(auth.runtime, batch.ID, overwrite)
	}
	sanitized := sanitizeMCPFileTransferBatch(batch, true)
	hint := "Poll get_file_transfer_batch for upload progress. File contents are transferred by the local MCP process and are not returned in tool results."
	retryAfter := 3
	if sanitized.Status == filetransfer.StatusPendingApproval {
		hint = "This upload queue is waiting for local approval in AIPermission Transfer Center. Poll get_file_transfer_batch; only approved files will be written to the remote server."
		retryAfter = 5
	}
	writeJSON(w, http.StatusAccepted, mcpFileTransferActionResponse{
		Status:            sanitized.Status,
		ServerID:          serverID,
		ServerName:        serverName,
		Batch:             &sanitized,
		RetryAfterSeconds: retryAfter,
		AssistantHint:     hint,
	})
}

func (s mcpHandlers) mcpDownloadFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	if s.rejectStoppedMCP(w, auth.runtime) {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	batch, err := auth.runtime.fileTransfers.GetBatch(r.Context(), id)
	if errors.Is(err, filetransfer.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file transfer batch not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	if _, ok := s.requireMCPTransferControl(w, r.Context(), auth.runtime, auth.TokenID, batch.ServerID); !ok {
		return
	}
	if batch.Source != filetransfer.SourceMCP {
		writeError(w, http.StatusForbidden, "MCP can only save downloads started by MCP")
		return
	}
	fileTransferHandlers{s.Server}.serveDownloadBatch(w, r, batch)
}

func (s mcpHandlers) mcpPauseFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	s.mcpControlFileTransferBatch(w, r, "pause")
}

func (s mcpHandlers) mcpResumeFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	s.mcpControlFileTransferBatch(w, r, "resume")
}

func (s mcpHandlers) mcpCancelFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	s.mcpControlFileTransferBatch(w, r, "cancel")
}

func (s mcpHandlers) mcpControlFileTransferBatch(w http.ResponseWriter, r *http.Request, action string) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return
	}
	if s.rejectStoppedMCP(w, auth.runtime) {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	batch, err := auth.runtime.fileTransfers.GetBatch(r.Context(), id)
	if errors.Is(err, filetransfer.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file transfer batch not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	serverName, ok := s.requireMCPTransferControl(w, r.Context(), auth.runtime, auth.TokenID, batch.ServerID)
	if !ok {
		return
	}

	switch action {
	case "pause":
		control := auth.runtime.batchControl(id)
		if control == nil {
			writeError(w, http.StatusConflict, "file transfer batch is not active")
			return
		}
		if !control.Pause() {
			writeError(w, http.StatusConflict, "file transfer batch is already paused")
			return
		}
		changed, err := auth.runtime.fileTransfers.PauseBatch(context.Background(), id)
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
	case "resume":
		control := auth.runtime.batchControl(id)
		if control == nil {
			writeError(w, http.StatusConflict, "file transfer batch is not active")
			return
		}
		changed, err := auth.runtime.fileTransfers.ResumeBatch(context.Background(), id)
		if err != nil {
			writeInternalError(w)
			return
		}
		if !changed {
			writeError(w, http.StatusConflict, "file transfer batch is not paused")
			return
		}
		control.Resume()
	case "cancel":
		auth.runtime.cancelBatch(id)
		if control := auth.runtime.batchControl(id); control != nil {
			control.Resume()
		}
		changed, err := auth.runtime.fileTransfers.CancelBatch(context.Background(), id, "canceled by MCP token")
		if err != nil {
			writeInternalError(w)
			return
		}
		if changed {
			fileTransferHandlers{s.Server}.cleanupBatchTemps(auth.runtime, id)
		}
	default:
		writeInternalError(w)
		return
	}
	s.writeAudit(context.Background(), auth.runtime, "mcp", int64Ptr(auth.TokenID), batch.ServerID, "mcp.file_transfer.batch."+action, map[string]any{"batch_id": id})
	updated, err := auth.runtime.fileTransfers.GetBatch(r.Context(), id)
	if err != nil {
		writeInternalError(w)
		return
	}
	sanitized := sanitizeMCPFileTransferBatch(updated, true)
	writeJSON(w, http.StatusOK, mcpFileTransferActionResponse{
		Status:     sanitized.Status,
		ServerID:   batch.ServerID,
		ServerName: serverName,
		Batch:      &sanitized,
	})
}

func (s mcpHandlers) mcpAllowedServerIDs(ctx context.Context, runtime *databaseRuntime, tokenID int64) ([]int64, error) {
	rows, err := runtime.database.QueryContext(ctx, `
		SELECT server_id
		FROM token_server_permissions
		WHERE token_id = ? AND execution_rule != ?
			AND (COALESCE(expires_at, '') = '' OR expires_at > ?)
		ORDER BY server_id`,
		tokenID,
		tokens.RuleBlocked,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s mcpHandlers) mcpCanSeeServer(ctx context.Context, runtime *databaseRuntime, tokenID int64, serverID int64) (bool, error) {
	_, rule, allowed, err := s.mcpPermission(ctx, runtime, tokenID, serverID)
	if err != nil {
		return false, err
	}
	return allowed && rule != tokens.RuleBlocked, nil
}

func (s mcpHandlers) requireMCPTransferControl(w http.ResponseWriter, ctx context.Context, runtime *databaseRuntime, tokenID int64, serverID int64) (string, bool) {
	permission, ok := s.mcpTransferStartPermission(w, ctx, runtime, tokenID, serverID)
	if !ok {
		return "", false
	}
	if permission.Rule != tokens.RuleAlwaysRun {
		writeJSON(w, http.StatusOK, mcpFileTransferActionResponse{
			Status:   "blocked",
			ServerID: serverID,
			Error:    "MCP file transfer control requires always_run permission for this server. Prompt-required transfer decisions must be made from the local AIPermission UI.",
		})
		return "", false
	}
	return permission.ServerName, true
}

func (s mcpHandlers) mcpTransferStartPermission(w http.ResponseWriter, ctx context.Context, runtime *databaseRuntime, tokenID int64, serverID int64) (mcpTransferPermissionResult, bool) {
	if serverID < 1 {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return mcpTransferPermissionResult{}, false
	}
	serverName, rule, allowed, err := s.mcpPermission(ctx, runtime, tokenID, serverID)
	if err != nil {
		writeInternalError(w)
		return mcpTransferPermissionResult{}, false
	}
	if !allowed || rule == tokens.RuleBlocked {
		writeJSON(w, http.StatusOK, mcpFileTransferActionResponse{
			Status:   "blocked",
			ServerID: serverID,
			Error:    "This token is blocked from managing file transfers on this server",
		})
		return mcpTransferPermissionResult{}, false
	}
	if rule != tokens.RuleAlwaysRun && rule != tokens.RuleApprovalRequired {
		writeJSON(w, http.StatusOK, mcpFileTransferActionResponse{
			Status:   "blocked",
			ServerID: serverID,
			Error:    "This token cannot manage file transfers on this server",
		})
		return mcpTransferPermissionResult{}, false
	}
	return mcpTransferPermissionResult{ServerName: serverName, Rule: rule}, true
}

func parseMCPTransferListPagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	limit := defaultMCPFileTransferLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return 0, 0, false
		}
		limit = value
	}
	if limit > maxMCPFileTransferLimit {
		limit = maxMCPFileTransferLimit
	}
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			writeError(w, http.StatusBadRequest, "invalid offset")
			return 0, 0, false
		}
		offset = value
	}
	return limit, offset, true
}

func validMCPTransferFilter(w http.ResponseWriter, direction string, status string) bool {
	if direction != "" && direction != filetransfer.DirectionUpload && direction != filetransfer.DirectionDownload {
		writeError(w, http.StatusBadRequest, "invalid direction")
		return false
	}
	if status != "" && !validFileTransferStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return false
	}
	return true
}

func sanitizeMCPFileTransferItems(items []filetransfer.Record) []mcpFileTransferItem {
	result := make([]mcpFileTransferItem, 0, len(items))
	for _, item := range items {
		result = append(result, sanitizeMCPFileTransferItem(item))
	}
	return result
}

func sanitizeMCPFileTransferItem(item filetransfer.Record) mcpFileTransferItem {
	return mcpFileTransferItem{
		ID:               item.ID,
		BatchID:          item.BatchID,
		QueueIndex:       item.QueueIndex,
		ServerID:         item.ServerID,
		ServerName:       item.ServerName,
		Direction:        item.Direction,
		Source:           item.Source,
		Status:           item.Status,
		RemotePath:       item.RemotePath,
		FileName:         item.FileName,
		SizeBytes:        item.SizeBytes,
		TransferredBytes: item.TransferredBytes,
		BytesPerSecond:   item.BytesPerSecond,
		ETASeconds:       item.ETASeconds,
		ChecksumSHA256:   item.ChecksumSHA256,
		Error:            item.Error,
		CreatedAt:        item.CreatedAt,
		StartedAt:        item.StartedAt,
		CompletedAt:      item.CompletedAt,
		UpdatedAt:        item.UpdatedAt,
	}
}

func sanitizeMCPFileTransferBatches(items []filetransfer.BatchRecord, includeItems bool) []mcpFileTransferBatchItem {
	result := make([]mcpFileTransferBatchItem, 0, len(items))
	for _, item := range items {
		result = append(result, sanitizeMCPFileTransferBatch(item, includeItems))
	}
	return result
}

func sanitizeMCPFileTransferBatch(item filetransfer.BatchRecord, includeItems bool) mcpFileTransferBatchItem {
	result := mcpFileTransferBatchItem{
		ID:               item.ID,
		ServerID:         item.ServerID,
		ServerName:       item.ServerName,
		Direction:        item.Direction,
		Source:           item.Source,
		Status:           item.Status,
		ArchiveName:      item.ArchiveName,
		ApprovalNote:     item.ApprovalNote,
		Overwrite:        item.Overwrite,
		TotalItems:       item.TotalItems,
		CompletedItems:   item.CompletedItems,
		FailedItems:      item.FailedItems,
		CanceledItems:    item.CanceledItems,
		SizeBytes:        item.SizeBytes,
		TransferredBytes: item.TransferredBytes,
		BytesPerSecond:   item.BytesPerSecond,
		ETASeconds:       item.ETASeconds,
		Error:            item.Error,
		CreatedAt:        item.CreatedAt,
		StartedAt:        item.StartedAt,
		CompletedAt:      item.CompletedAt,
		UpdatedAt:        item.UpdatedAt,
	}
	if includeItems {
		result.Items = sanitizeMCPFileTransferItems(item.Items)
	}
	return result
}

func pathDirForMCPBrowse(remotePath string) string {
	parent := path.Dir(remotePath)
	if parent == "." || parent == remotePath {
		return "/"
	}
	return parent
}
