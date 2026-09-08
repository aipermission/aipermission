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

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
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
	usedNames := map[string]int{}
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

func UniqueArchiveEntryName(name, remotePath, archiveRoot string, used map[string]int) string {
	base := RelativeArchiveEntryPath(remotePath, archiveRoot)
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
	for suffix := 2; used[archiveCollisionKey(candidate)] > 0; suffix++ {
		candidate = directory + fmt.Sprintf("%s-%d%s", stem, suffix, ext)
	}
	used[archiveCollisionKey(candidate)] = 1
	return candidate
}

func archiveCollisionKey(value string) string {
	return cases.Fold().String(norm.NFC.String(strings.TrimRight(value, " .")))
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
	value = SafeFileName(value)
	var builder strings.Builder
	for _, char := range value {
		if strings.ContainsRune(`<>:"|?*`, char) {
			builder.WriteRune('_')
		} else {
			builder.WriteRune(char)
		}
	}
	value = builder.String()
	trailing := len(value) - len(strings.TrimRight(value, " ."))
	if trailing > 0 {
		value = strings.TrimRight(value, " .") + strings.Repeat("_", trailing)
	}
	if value == "" || value == "." || value == ".." {
		return "aipermission-file"
	}
	base := strings.TrimRight(strings.SplitN(value, ".", 2)[0], " .")
	if windowsReservedArchiveNames[strings.ToLower(base)] {
		value = "_" + value
	}
	return value
}

var windowsReservedArchiveNames = map[string]bool{
	"aux": true, "con": true, "nul": true, "prn": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
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
	header.Name = safeArchiveEntryPath(name)
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
	cleanName := strings.TrimLeft(SafeFileName(fileName), "/")
	if cleanName == "" {
		cleanName = "file"
	}
	if remoteDir == "/" {
		return "/" + cleanName
	}
	return strings.TrimRight(remoteDir, "/") + "/" + cleanName
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
	value = strings.ReplaceAll(value, "\\", "/")
	value = path.Base(value)
	if value == "" || value == "/" || value == "." || value == ".." {
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
	result := builder.String()
	if result == "" {
		return "aipermission-file"
	}
	if len([]rune(result)) > 160 {
		return string([]rune(result)[:160])
	}
	return result
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
