package api

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

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
	archiveRoot := commonRemoteArchiveRoot(batch.Items)
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
		entryName := uniqueArchiveEntryName(item.FileName, item.RemotePath, archiveRoot, usedNames)
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

func uniqueArchiveEntryName(name string, remotePath string, archiveRoot string, used map[string]int) string {
	base := relativeArchiveEntryPath(remotePath, archiveRoot)
	if base == "" {
		base = safeArchiveEntryPath(name)
	}
	directory, fileName := path.Split(base)
	ext := path.Ext(fileName)
	stem := strings.TrimSuffix(fileName, ext)
	if stem == "" {
		stem = "file"
	}
	candidate := base
	for suffix := 2; used[candidate] > 0; suffix++ {
		candidate = directory + fmt.Sprintf("%s-%d%s", stem, suffix, ext)
	}
	used[candidate] = 1
	return candidate
}

func commonRemoteArchiveRoot(items []filetransfer.Record) string {
	var common []string
	initialized := false
	for _, item := range items {
		if item.Status != filetransfer.StatusCompleted {
			continue
		}
		directory := strings.Trim(path.Dir("/"+strings.TrimLeft(item.RemotePath, "/")), "/")
		parts := []string{}
		if directory != "" && directory != "." {
			parts = strings.Split(directory, "/")
		}
		if !initialized {
			common = append([]string(nil), parts...)
			initialized = true
			continue
		}
		limit := len(common)
		if len(parts) < limit {
			limit = len(parts)
		}
		matched := 0
		for matched < limit && common[matched] == parts[matched] {
			matched++
		}
		common = common[:matched]
	}
	if len(common) == 0 {
		return "/"
	}
	return "/" + strings.Join(common, "/")
}

func relativeArchiveEntryPath(remotePath string, archiveRoot string) string {
	remote := path.Clean("/" + strings.TrimLeft(remotePath, "/"))
	root := path.Clean("/" + strings.TrimLeft(archiveRoot, "/"))
	relative := strings.TrimLeft(remote, "/")
	if root != "/" {
		prefix := strings.TrimRight(root, "/") + "/"
		if !strings.HasPrefix(remote, prefix) {
			return ""
		}
		relative = strings.TrimPrefix(remote, prefix)
	}
	return safeArchiveEntryPath(relative)
}

func safeArchiveEntryPath(value string) string {
	cleaned := path.Clean(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")))
	if cleaned == "." || cleaned == "/" || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "aipermission-file"
	}
	parts := strings.Split(strings.TrimLeft(cleaned, "/"), "/")
	for index, part := range parts {
		parts[index] = safeFileName(part)
	}
	return strings.Join(parts, "/")
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
	header.Name = safeArchiveEntryPath(name)
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
	setAttachmentHeaders(w, fileName, contentType)
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
