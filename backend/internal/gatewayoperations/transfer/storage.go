package transfer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func (s Runner) StageUploadFile(reader io.Reader) (string, int64, string, error) {
	root, err := s.EnsureTempRoot()
	if err != nil {
		return "", 0, "", err
	}
	return filetransfer.StageUpload(root, reader)
}

func (s Runner) ReserveDownloadTempFile() (string, error) {
	root, err := s.EnsureTempRoot()
	if err != nil {
		return "", err
	}
	return filetransfer.ReserveDownload(root)
}

func (s Runner) RemoveTransferTemp(runtime *Runtime, transferID int64) {
	item, err := runtime.store.Get(context.Background(), transferID)
	if err == nil {
		s.RemoveTempPath(item.TempPath)
	}
}

func (s Runner) RemoveTempPath(value string) {
	if value != "" && s.TempPathAllowed(value) {
		_ = os.Remove(value)
	}
}

func (s Runner) transferTerminalDurable(runtime *Runtime, transferID int64) bool {
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferPersistenceAttemptTimeout)
	defer cancel()
	item, err := runtime.store.Get(ctx, transferID)
	return err == nil && fileTransferTerminal(item.Status)
}

func fileTransferTerminal(status string) bool {
	return status == filetransfer.StatusCompleted || status == filetransfer.StatusFailed || status == filetransfer.StatusCanceled
}

func (s Runner) CleanupBatchTemps(runtime *Runtime, batchID int64) {
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
		if value != "" && s.TempPathAllowed(value) {
			_ = os.Remove(value)
		}
	}
}
func (s Runner) cleanupBatchTempsIfDurable(runtime *Runtime, batchID int64, durable bool) {
	if durable {
		s.CleanupBatchTemps(runtime, batchID)
	}
}
func (s Runner) scheduleBatchTempCleanup(batch filetransfer.BatchRecord) {
	s.scheduleTransferTempCleanup(batch.ArchivePath)
	for _, item := range batch.Items {
		s.scheduleTransferTempCleanup(item.TempPath)
	}
}

func (s Runner) CreateDownloadArchive(batch filetransfer.BatchRecord) (string, error) {
	root, err := s.EnsureTempRoot()
	if err != nil {
		return "", err
	}
	return filetransfer.CreateDownloadArchive(root, batch)
}

func (s Runner) EnsureTempRoot() (string, error) {
	root := s.fileTransferTempRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create file transfer temp directory: %w", err)
	}
	return root, nil
}

func (s Runner) fileTransferTempRoot() string {
	return filepath.Join(filepath.Dir(s.dataPath), "file-transfers")
}

func (s Runner) TempPathAllowed(value string) bool {
	return filetransfer.TempPathAllowed(s.fileTransferTempRoot(), value)
}

func (s Runner) scheduleTransferTempCleanup(value string) {
	filetransfer.ScheduleTempCleanup(s.fileTransferTempRoot(), value, s.tempTTL)
}
