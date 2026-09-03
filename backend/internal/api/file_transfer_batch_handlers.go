package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func (s fileTransferHandlers) listFileTransferBatches(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	page, err := parsePageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter := filetransfer.BatchListFilter{
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
	items, total, err := runtime.fileTransfers.ListBatches(r.Context(), filter)
	if err != nil {
		writeInternalError(w)
		return
	}
	for index := range items {
		batchItems, err := runtime.fileTransfers.ListBatchItems(r.Context(), items[index].ID)
		if err != nil {
			writeInternalError(w)
			return
		}
		items[index].Items = batchItems
	}
	writeJSON(w, http.StatusOK, makePageResponse(items, total, page))
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
	s.writeObservationAudit(context.Background(), runtime, "user", nil, 0, "file_transfer.batch.resumed", map[string]any{"batch_id": id})
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
	if !decodeJSON(w, r, &request) {
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
	item, err := runtime.fileTransfers.GetBatch(r.Context(), id)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s fileTransferHandlers) approveFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request approveFileTransferBatchRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	batch, rejected, err := runtime.fileTransfers.ApproveBatch(r.Context(), id, filetransfer.BatchApprovalRequest{
		ApprovedItemIDs: request.ItemIDs,
		Note:            request.Note,
	})
	if errors.Is(err, filetransfer.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file transfer batch not found")
		return
	}
	if errors.Is(err, filetransfer.ErrInvalidArgument) {
		writeError(w, http.StatusBadRequest, "item_ids must contain pending approval item ids")
		return
	}
	if errors.Is(err, filetransfer.ErrInvalidState) {
		writeError(w, http.StatusConflict, "file transfer batch is not waiting for approval")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	for _, item := range rejected {
		if item.TempPath != "" && s.tempPathAllowed(item.TempPath) {
			_ = os.Remove(item.TempPath)
		}
	}
	s.writeObservationAudit(r.Context(), runtime, "user", nil, batch.RuntimeID, "file_transfer.batch.approved", map[string]any{
		"batch_id":       id,
		"approved_items": len(request.ItemIDs),
		"rejected_items": len(rejected),
		"note":           strings.TrimSpace(request.Note),
	})
	if len(request.ItemIDs) > 0 {
		go s.runTransferBatch(runtime, batch.ID, batch.Overwrite)
	}
	writeJSON(w, http.StatusOK, batch)
}

func (s fileTransferHandlers) declineFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request declineFileTransferBatchRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	batch, rejected, err := runtime.fileTransfers.DeclineBatch(r.Context(), id, request.Note)
	if errors.Is(err, filetransfer.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file transfer batch not found")
		return
	}
	if errors.Is(err, filetransfer.ErrInvalidState) {
		writeError(w, http.StatusConflict, "file transfer batch is not waiting for approval")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	for _, item := range rejected {
		if item.TempPath != "" && s.tempPathAllowed(item.TempPath) {
			_ = os.Remove(item.TempPath)
		}
	}
	s.writeObservationAudit(r.Context(), runtime, "user", nil, batch.RuntimeID, "file_transfer.batch.declined", map[string]any{
		"batch_id": id,
		"items":    len(rejected),
		"note":     strings.TrimSpace(request.Note),
	})
	writeJSON(w, http.StatusOK, batch)
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
	s.serveDownloadBatch(w, r, batch)
}

func (s fileTransferHandlers) serveDownloadBatch(w http.ResponseWriter, r *http.Request, batch filetransfer.BatchRecord) {
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
