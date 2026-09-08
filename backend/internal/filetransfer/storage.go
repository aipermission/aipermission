package filetransfer

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/archivepath"
	"github.com/aipermission/aipermission/backend/internal/localfilename"
)

func StageUpload(root string, reader io.Reader) (string, int64, string, error) {
	temp, err := os.CreateTemp(root, "upload-*")
	if err != nil {
		return "", 0, "", fmt.Errorf("create temporary upload file: %w", err)
	}
	tempPath := temp.Name()
	defer temp.Close()
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(temp, digest), reader)
	if err != nil {
		_ = os.Remove(tempPath)
		return "", 0, "", fmt.Errorf("stage upload file: %w", err)
	}
	return tempPath, size, hex.EncodeToString(digest.Sum(nil)), nil
}

func ReserveDownload(root string) (string, error) {
	temp, err := os.CreateTemp(root, "download-*")
	if err != nil {
		return "", fmt.Errorf("create temporary download file: %w", err)
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("close temporary download file: %w", err)
	}
	return tempPath, nil
}

func CreateDownloadArchive(root string, batch BatchRecord) (string, error) {
	temp, err := os.CreateTemp(root, "archive-*.zip")
	if err != nil {
		return "", fmt.Errorf("create temporary download archive: %w", err)
	}
	archivePath := temp.Name()
	zipWriter := zip.NewWriter(temp)
	usedNames := archivepath.NewTracker()
	archiveRoot := commonRemoteArchiveRoot(batch.Items)
	for _, item := range batch.Items {
		if item.Status != StatusCompleted {
			continue
		}
		if item.TempPath == "" || !TempPathAllowed(root, item.TempPath) {
			_ = zipWriter.Close()
			_ = temp.Close()
			_ = os.Remove(archivePath)
			return "", fmt.Errorf("download file is no longer available")
		}
		if err := addFileToZip(zipWriter, item.TempPath, UniqueArchiveEntryName(item.FileName, item.RemotePath, archiveRoot, usedNames)); err != nil {
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

func UniqueArchiveEntryName(name, remotePath, archiveRoot string, used *archivepath.Tracker) string {
	base := RelativeArchiveEntryPath(remotePath, archiveRoot)
	if base == "" {
		base = safeArchiveEntryPath(name)
	}
	parts := strings.Split(safeArchiveEntryPath(base), "/")
	original := append([]string(nil), parts...)
	suffixes := make([]int, len(parts))
	for {
		conflict := used.ConflictIndex(parts)
		if conflict < 0 {
			used.Register(parts)
			return strings.Join(parts, "/")
		}
		suffixes[conflict]++
		parts[conflict] = archiveComponentWithSuffix(original[conflict], suffixes[conflict]+1)
	}
}

func archiveComponentWithSuffix(value string, suffix int) string {
	suffixText := fmt.Sprintf("-%d", suffix)
	ext := path.Ext(value)
	stem := strings.TrimSuffix(value, ext)
	if stem == "" {
		stem = "file"
	}
	stem = localfilename.FitWithSuffix(stem, suffixText+ext)
	if stem == "" {
		ext = ""
		stem = localfilename.FitWithSuffix("file", suffixText)
	}
	return stem + suffixText + ext
}

func RelativeArchiveEntryPath(remotePath, archiveRoot string) string {
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

func commonRemoteArchiveRoot(items []Record) string {
	var common []string
	initialized := false
	for _, item := range items {
		if item.Status != StatusCompleted {
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
		limit := min(len(common), len(parts))
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

func safeArchiveEntryPath(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	if strings.TrimSpace(value) == "" {
		return "aipermission-file"
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == "/" || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "aipermission-file"
	}
	parts := strings.Split(strings.TrimLeft(cleaned, "/"), "/")
	for index, part := range parts {
		parts[index] = safeArchiveComponent(part)
	}
	return strings.Join(parts, "/")
}

func safeArchiveComponent(value string) string {
	return localfilename.Safe(value, "aipermission-file")
}

func addFileToZip(zipWriter *zip.Writer, filePath, name string) error {
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
	if name == "" || safeArchiveEntryPath(name) != name {
		return fmt.Errorf("archive entry name is not canonical")
	}
	header.Name = name
	header.Method = zip.Deflate
	switch strings.ToLower(filepath.Ext(header.Name)) {
	case ".zip", ".gz", ".tgz", ".bz2", ".xz", ".7z", ".rar":
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

func CleanupPaths(paths []string) {
	for _, value := range paths {
		if value != "" {
			_ = os.Remove(value)
		}
	}
}

func TempPathAllowed(root, value string) bool {
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(value)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(rootPath, target)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func ScheduleTempCleanup(root, value string, ttl time.Duration) {
	if value == "" || !TempPathAllowed(root, value) {
		return
	}
	time.AfterFunc(ttl, func() { _ = os.Remove(value) })
}

func JoinRemoteFilePath(remoteDir, fileName string) string {
	cleanName := strings.TrimLeft(safeRemoteUploadName(fileName), "/")
	if cleanName == "" {
		cleanName = "file"
	}
	if remoteDir == "/" {
		return "/" + cleanName
	}
	return strings.TrimRight(remoteDir, "/") + "/" + cleanName
}

func safeRemoteUploadName(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	value = path.Base(value)
	if value == "" || value == "/" || value == "." || value == ".." {
		return "file"
	}
	var builder strings.Builder
	for _, character := range value {
		if unicode.IsControl(character) || character == '/' || character == '\\' {
			builder.WriteRune('_')
			continue
		}
		builder.WriteRune(character)
	}
	return localfilename.FitWithSuffix(builder.String(), "")
}

func SpeedAndETA(transferred, total int64, elapsed time.Duration) (int64, int64) {
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
	return bytesPerSecond, (total - transferred) / bytesPerSecond
}

func NormalizeRemoteFilePath(value string) (string, error) {
	if err := validatePortableRemotePath(value, "remote_path"); err != nil {
		return "", err
	}
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

func NormalizeRemoteDirectoryPath(value string) (string, error) {
	if value != "" {
		if err := validatePortableRemotePath(value, "path"); err != nil {
			return "", err
		}
	}
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

func NormalizeRelativeTransferPath(value string) (string, error) {
	if err := validatePortableRemotePath(strings.ReplaceAll(value, "\\", "/"), "relative upload path"); err != nil {
		return "", err
	}
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || len([]rune(value)) > 4096 {
		return "", fmt.Errorf("relative upload path is invalid")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("relative upload path cannot contain control characters")
		}
	}
	if path.IsAbs(value) {
		return "", fmt.Errorf("relative upload path must not be absolute")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("relative upload path cannot leave the selected directory")
	}
	return cleaned, nil
}

func JoinRemoteRelativePath(remoteDir, relativePath string) string {
	if remoteDir == "/" {
		return "/" + strings.TrimLeft(relativePath, "/")
	}
	return strings.TrimRight(remoteDir, "/") + "/" + strings.TrimLeft(relativePath, "/")
}

func SafeFileName(value string) string {
	return localfilename.Safe(value, "aipermission-file")
}

func ValidateFileName(value string) error {
	if value == "" {
		return fmt.Errorf("file name is required")
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("file name cannot contain only whitespace")
	}
	if value == "." || value == ".." {
		return fmt.Errorf("file name cannot be %q", value)
	}
	if len([]rune(value)) > 160 {
		return fmt.Errorf("file name must be 160 characters or fewer")
	}
	if !utf8.ValidString(value) || len(value) > localfilename.MaxUTF8Bytes {
		return fmt.Errorf("file name must be valid UTF-8 and %d bytes or fewer", localfilename.MaxUTF8Bytes)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("file name cannot contain control characters")
		}
		if r == '/' || r == '\\' {
			return fmt.Errorf("file name cannot contain path separators")
		}
	}
	return nil
}

func validatePortableRemotePath(value, label string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", label)
	}
	if len(value) > 4096 {
		return fmt.Errorf("%s must be 4096 bytes or fewer", label)
	}
	for _, component := range strings.Split(value, "/") {
		if len(component) > localfilename.MaxUTF8Bytes {
			return fmt.Errorf("%s components must be %d bytes or fewer", label, localfilename.MaxUTF8Bytes)
		}
	}
	return nil
}
