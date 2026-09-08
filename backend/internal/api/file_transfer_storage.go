package api

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/httpattachment"
)

func (s fileTransferHandlers) stageUploadFile(reader io.Reader) (string, int64, string, error) {
	root, err := s.ensureFileTransferTempRoot()
	if err != nil {
		return "", 0, "", err
	}
	return filetransfer.StageUpload(root, reader)
}

func (s fileTransferHandlers) reserveDownloadTempFile() (string, error) {
	root, err := s.ensureFileTransferTempRoot()
	if err != nil {
		return "", err
	}
	return filetransfer.ReserveDownload(root)
}

func (s fileTransferHandlers) removeTransferTemp(runtime *databaseRuntime, transferID int64) {
	item, err := runtime.fileTransfers.Get(context.Background(), transferID)
	if err == nil && item.TempPath != "" && s.tempPathAllowed(item.TempPath) {
		_ = os.Remove(item.TempPath)
	}
}

func (s fileTransferHandlers) cleanupBatchTemps(runtime *databaseRuntime, batchID int64) {
	batch, err := runtime.fileTransfers.GetBatch(context.Background(), batchID)
	if err != nil {
		return
	}
	paths := make([]string, 0, len(batch.Items)+1)
	paths = append(paths, batch.ArchivePath)
	for _, item := range batch.Items {
		paths = append(paths, item.TempPath)
	}
	for _, value := range paths {
		if value != "" && s.tempPathAllowed(value) {
			_ = os.Remove(value)
		}
	}
}

func (s fileTransferHandlers) scheduleBatchItemTempCleanup(batch filetransfer.BatchRecord) {
	for _, item := range batch.Items {
		s.scheduleTransferTempCleanup(item.TempPath)
	}
}

func (s fileTransferHandlers) createDownloadArchive(batch filetransfer.BatchRecord) (string, error) {
	root, err := s.ensureFileTransferTempRoot()
	if err != nil {
		return "", err
	}
	return filetransfer.CreateDownloadArchive(root, batch)
}

func uniqueArchiveEntryName(name, remotePath, archiveRoot string, used map[string]int) string {
	return filetransfer.UniqueArchiveEntryName(name, remotePath, archiveRoot, used)
}

func relativeArchiveEntryPath(remotePath, archiveRoot string) string {
	return filetransfer.RelativeArchiveEntryPath(remotePath, archiveRoot)
}

func setDownloadHeaders(w http.ResponseWriter, fileName string) {
	contentType := mime.TypeByExtension(filepath.Ext(fileName))
	httpattachment.SetHeaders(w, fileName, contentType)
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
	filetransfer.CleanupPaths(paths)
}

func joinRemoteFilePath(remoteDir, fileName string) string {
	return filetransfer.JoinRemoteFilePath(remoteDir, fileName)
}

func transferSpeedAndETA(transferred, total int64, elapsed time.Duration) (int64, int64) {
	return filetransfer.SpeedAndETA(transferred, total, elapsed)
}

func (s fileTransferHandlers) tempPathAllowed(value string) bool {
	return filetransfer.TempPathAllowed(s.fileTransferTempRoot(), value)
}

func (s fileTransferHandlers) scheduleTransferTempCleanup(value string) {
	filetransfer.ScheduleTempCleanup(s.fileTransferTempRoot(), value, fileTransferTempTTL)
}
