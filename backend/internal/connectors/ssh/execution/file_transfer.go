package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"time"

	"github.com/pkg/sftp"
)

type TransferProgress func(transferred int64, total int64)

type TransferOptions struct {
	Progress TransferProgress
	Wait     func(context.Context) error
}

type TransferResult struct {
	Bytes          int64
	Size           int64
	ChecksumSHA256 string
	DurationMS     int64
}

type RemoteFileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

type RemotePathStatus struct {
	Exists bool   `json:"exists"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
}

func StatRemotePath(ctx context.Context, target Target, remotePath string) (RemotePathStatus, error) {
	client, sshClient, err := sftpClient(ctx, target)
	if err != nil {
		return RemotePathStatus{}, err
	}
	defer sshClient.Close()
	defer client.Close()
	closeOnContext(ctx, sshClient)

	info, err := client.Stat(remotePath)
	if err != nil {
		if os.IsNotExist(err) {
			return RemotePathStatus{Exists: false}, nil
		}
		return RemotePathStatus{}, fmt.Errorf("stat remote path: %w", err)
	}
	entryType := "file"
	if info.IsDir() {
		entryType = "directory"
	} else if !info.Mode().IsRegular() {
		entryType = "other"
	}
	return RemotePathStatus{Exists: true, Type: entryType, Size: info.Size()}, nil
}

func ListRemoteDirectory(ctx context.Context, target Target, remotePath string) ([]RemoteFileEntry, error) {
	client, sshClient, err := sftpClient(ctx, target)
	if err != nil {
		return nil, err
	}
	defer sshClient.Close()
	defer client.Close()
	closeOnContext(ctx, sshClient)

	entries, err := client.ReadDir(remotePath)
	if err != nil {
		return nil, fmt.Errorf("read remote directory: %w", err)
	}
	items := make([]RemoteFileEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." {
			continue
		}
		entryPath := path.Join(remotePath, name)
		if remotePath == "/" {
			entryPath = "/" + name
		}
		entryType := "file"
		if entry.IsDir() {
			entryType = "directory"
		} else if !entry.Mode().IsRegular() {
			entryType = "other"
		}
		items = append(items, RemoteFileEntry{
			Name:       name,
			Path:       entryPath,
			Type:       entryType,
			Size:       entry.Size(),
			ModifiedAt: entry.ModTime().UTC().Format(time.RFC3339),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Type == "directory" && items[j].Type != "directory" {
			return true
		}
		if items[i].Type != "directory" && items[j].Type == "directory" {
			return false
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func UploadFile(ctx context.Context, target Target, localPath string, remotePath string, overwrite bool, progress TransferProgress) (TransferResult, error) {
	return UploadFileWithOptions(ctx, target, localPath, remotePath, overwrite, TransferOptions{Progress: progress})
}

func UploadFileWithOptions(ctx context.Context, target Target, localPath string, remotePath string, overwrite bool, options TransferOptions) (TransferResult, error) {
	started := time.Now()
	local, err := os.Open(localPath)
	if err != nil {
		return TransferResult{}, fmt.Errorf("open local file: %w", err)
	}
	defer local.Close()
	info, err := local.Stat()
	if err != nil {
		return TransferResult{}, fmt.Errorf("stat local file: %w", err)
	}
	if info.IsDir() {
		return TransferResult{}, fmt.Errorf("local path is a directory")
	}

	client, sshClient, err := sftpClient(ctx, target)
	if err != nil {
		return TransferResult{}, err
	}
	defer sshClient.Close()
	defer client.Close()
	closeOnContext(ctx, sshClient)

	if dir := path.Dir(remotePath); dir != "" && dir != "." && dir != "/" {
		if err := client.MkdirAll(dir); err != nil {
			return TransferResult{}, fmt.Errorf("create remote directory: %w", err)
		}
	}
	if !overwrite {
		if _, err := client.Stat(remotePath); err == nil {
			return TransferResult{}, fmt.Errorf("remote file already exists")
		} else if !os.IsNotExist(err) {
			return TransferResult{}, fmt.Errorf("stat remote file: %w", err)
		}
	}

	tempPath, remote, err := createRemoteUploadTemp(client, remotePath)
	if err != nil {
		return TransferResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			cleanupRemoteUploadTemp(target, tempPath)
		}
	}()

	copied, checksum, err := copyWithProgress(ctx, remote, local, info.Size(), options)
	closeErr := remote.Close()
	if err != nil {
		return TransferResult{}, fmt.Errorf("upload file: %w", err)
	}
	if closeErr != nil {
		return TransferResult{}, fmt.Errorf("close remote temporary file: %w", closeErr)
	}
	if err := ctx.Err(); err != nil {
		return TransferResult{}, err
	}
	if options.Wait != nil {
		if err := options.Wait(ctx); err != nil {
			return TransferResult{}, err
		}
	}
	if err := commitRemoteUpload(client, tempPath, remotePath, overwrite); err != nil {
		return TransferResult{}, err
	}
	committed = true
	if options.Progress != nil {
		options.Progress(copied, info.Size())
	}
	return TransferResult{
		Bytes:          copied,
		Size:           info.Size(),
		ChecksumSHA256: checksum,
		DurationMS:     time.Since(started).Milliseconds(),
	}, nil
}

type remoteUploadClient interface {
	Stat(string) (os.FileInfo, error)
	OpenFile(string, int) (*sftp.File, error)
	Rename(string, string) error
	PosixRename(string, string) error
	Remove(string) error
}

type remoteUploadCommitter interface {
	Stat(string) (os.FileInfo, error)
	Rename(string, string) error
	PosixRename(string, string) error
	Remove(string) error
}

func createRemoteUploadTemp(client remoteUploadClient, remotePath string) (string, *sftp.File, error) {
	for attempt := 0; attempt < 10; attempt++ {
		tempPath := remoteUploadTempPath(remotePath, attempt)
		remote, err := client.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if err == nil {
			return tempPath, remote, nil
		}
		if os.IsExist(err) {
			continue
		}
		return "", nil, fmt.Errorf("create remote temporary file: %w", err)
	}
	return "", nil, fmt.Errorf("create remote temporary file: exhausted unique names")
}

func remoteUploadTempPath(remotePath string, attempt int) string {
	dir := path.Dir(remotePath)
	base := path.Base(remotePath)
	if base == "" || base == "." || base == "/" {
		base = "file"
	}
	runes := []rune(base)
	if len(runes) > 96 {
		base = string(runes[:96])
	}
	name := fmt.Sprintf(".aipermission-upload-%s-%d-%d-%d.tmp", base, os.Getpid(), time.Now().UnixNano(), attempt)
	if dir == "." || dir == "" {
		return name
	}
	if dir == "/" {
		return "/" + name
	}
	return path.Join(dir, name)
}

func commitRemoteUpload(client remoteUploadCommitter, tempPath string, remotePath string, overwrite bool) error {
	if !overwrite {
		if _, err := client.Stat(remotePath); err == nil {
			return fmt.Errorf("remote file already exists")
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat remote file before commit: %w", err)
		}
		if err := client.Rename(tempPath, remotePath); err != nil {
			return fmt.Errorf("move uploaded file into place: %w", err)
		}
		return nil
	}
	if err := client.PosixRename(tempPath, remotePath); err == nil {
		return nil
	}
	if err := client.Rename(tempPath, remotePath); err == nil {
		return nil
	}
	if err := client.Remove(remotePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing remote file before overwrite: %w", err)
	}
	if err := client.Rename(tempPath, remotePath); err != nil {
		return fmt.Errorf("move uploaded file into place: %w", err)
	}
	return nil
}

func cleanupRemoteUploadTemp(target Target, tempPath string) {
	if tempPath == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, sshClient, err := sftpClient(ctx, target)
	if err != nil {
		return
	}
	defer sshClient.Close()
	defer client.Close()
	closeOnContext(ctx, sshClient)
	_ = client.Remove(tempPath)
}

func DownloadFile(ctx context.Context, target Target, remotePath string, localPath string, progress TransferProgress) (TransferResult, error) {
	return DownloadFileWithOptions(ctx, target, remotePath, localPath, TransferOptions{Progress: progress})
}

func DownloadFileWithOptions(ctx context.Context, target Target, remotePath string, localPath string, options TransferOptions) (TransferResult, error) {
	started := time.Now()
	client, sshClient, err := sftpClient(ctx, target)
	if err != nil {
		return TransferResult{}, err
	}
	defer sshClient.Close()
	defer client.Close()
	closeOnContext(ctx, sshClient)

	remote, err := client.Open(remotePath)
	if err != nil {
		return TransferResult{}, fmt.Errorf("open remote file: %w", err)
	}
	defer remote.Close()
	info, err := remote.Stat()
	if err != nil {
		return TransferResult{}, fmt.Errorf("stat remote file: %w", err)
	}
	if info.IsDir() {
		return TransferResult{}, fmt.Errorf("remote path is a directory")
	}

	local, err := os.Create(localPath)
	if err != nil {
		return TransferResult{}, fmt.Errorf("create local file: %w", err)
	}
	defer local.Close()

	copied, checksum, err := copyWithProgress(ctx, local, remote, info.Size(), options)
	if err != nil {
		return TransferResult{}, fmt.Errorf("download file: %w", err)
	}
	if options.Progress != nil {
		options.Progress(copied, info.Size())
	}
	return TransferResult{
		Bytes:          copied,
		Size:           info.Size(),
		ChecksumSHA256: checksum,
		DurationMS:     time.Since(started).Milliseconds(),
	}, nil
}

func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, total int64, options TransferOptions) (int64, string, error) {
	hasher := sha256.New()
	buffer := make([]byte, 128*1024)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return copied, "", err
		}
		if options.Wait != nil {
			if err := options.Wait(ctx); err != nil {
				return copied, "", err
			}
		}
		nr, er := src.Read(buffer)
		if nr > 0 {
			chunk := buffer[:nr]
			nw, ew := dst.Write(chunk)
			if nw > 0 {
				_, _ = hasher.Write(chunk[:nw])
				copied += int64(nw)
				if options.Progress != nil {
					options.Progress(copied, total)
				}
			}
			if ew != nil {
				return copied, "", ew
			}
			if nr != nw {
				return copied, "", io.ErrShortWrite
			}
		}
		if er != nil {
			if er == io.EOF {
				break
			}
			return copied, "", er
		}
	}
	return copied, hex.EncodeToString(hasher.Sum(nil)), nil
}

func sftpClient(ctx context.Context, target Target) (*sftp.Client, interface{ Close() error }, error) {
	sshClient, err := DialSSH(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	client, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, fmt.Errorf("start sftp client: %w", err)
	}
	return client, sshClient, nil
}

func closeOnContext(ctx context.Context, closer interface{ Close() error }) {
	if ctx.Done() == nil {
		return
	}
	go func() {
		<-ctx.Done()
		_ = closer.Close()
	}()
}

type progressReader struct {
	reader      io.Reader
	transferred int64
	total       int64
	fn          TransferProgress
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.transferred += int64(n)
		if r.fn != nil {
			r.fn(r.transferred, r.total)
		}
	}
	return n, err
}

type progressWriter struct {
	writer      io.Writer
	transferred int64
	total       int64
	fn          TransferProgress
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if n > 0 {
		w.transferred += int64(n)
		if w.fn != nil {
			w.fn(w.transferred, w.total)
		}
	}
	return n, err
}
