package filetransferhttp

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

func (s Handlers) stageUploadFile(reader io.Reader) (string, int64, string, error) {
	root, err := s.ensureFileTransferTempRoot()
	if err != nil {
		return "", 0, "", err
	}
	return filetransfer.StageUpload(root, reader)
}

func (s Handlers) reserveDownloadTempFile() (string, error) {
	root, err := s.ensureFileTransferTempRoot()
	if err != nil {
		return "", err
	}
	return filetransfer.ReserveDownload(root)
}

func (s Handlers) removeTransferTemp(runtime *Runtime, transferID int64) {
	item, err := runtime.store.Get(context.Background(), transferID)
	if err == nil {
		s.removeTransferTempPath(item.TempPath)
	}
}

func (s Handlers) removeTransferTempPath(value string) {
	if value != "" && s.tempPathAllowed(value) {
		_ = os.Remove(value)
	}
}

func (s Handlers) transferTerminalDurable(runtime *Runtime, transferID int64) bool {
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferPersistenceAttemptTimeout)
	defer cancel()
	item, err := runtime.store.Get(ctx, transferID)
	return err == nil && fileTransferTerminal(item.Status)
}

func fileTransferTerminal(status string) bool {
	return status == filetransfer.StatusCompleted || status == filetransfer.StatusFailed || status == filetransfer.StatusCanceled
}

func (s Handlers) cleanupBatchTemps(runtime *Runtime, batchID int64) {
	batch, err := runtime.store.GetBatch(context.Background(), batchID)
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
func (s Handlers) cleanupBatchTempsIfDurable(runtime *Runtime, batchID int64, durable bool) {
	if durable {
		s.cleanupBatchTemps(runtime, batchID)
	}
}
func (s Handlers) scheduleBatchTempCleanup(batch filetransfer.BatchRecord) {
	s.scheduleTransferTempCleanup(batch.ArchivePath)
	for _, item := range batch.Items {
		s.scheduleTransferTempCleanup(item.TempPath)
	}
}

func (s Handlers) createDownloadArchive(batch filetransfer.BatchRecord) (string, error) {
	root, err := s.ensureFileTransferTempRoot()
	if err != nil {
		return "", err
	}
	return filetransfer.CreateDownloadArchive(root, batch)
}

func setDownloadHeaders(w http.ResponseWriter, fileName string) {
	contentType := mime.TypeByExtension(filepath.Ext(fileName))
	httpattachment.SetHeaders(w, fileName, contentType)
}

func (s Handlers) ensureFileTransferTempRoot() (string, error) {
	root := s.fileTransferTempRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create file transfer temp directory: %w", err)
	}
	return root, nil
}

func (s Handlers) fileTransferTempRoot() string {
	return filepath.Join(filepath.Dir(s.dataPath), "file-transfers")
}

func cleanupTempPaths(paths []string) {
	filetransfer.CleanupPaths(paths)
}

func transferSpeedAndETA(transferred, total int64, elapsed time.Duration) (int64, int64) {
	return filetransfer.SpeedAndETA(transferred, total, elapsed)
}

func (s Handlers) tempPathAllowed(value string) bool {
	return filetransfer.TempPathAllowed(s.fileTransferTempRoot(), value)
}

func (s Handlers) scheduleTransferTempCleanup(value string) {
	filetransfer.ScheduleTempCleanup(s.fileTransferTempRoot(), value, fileTransferTempTTL)
}
