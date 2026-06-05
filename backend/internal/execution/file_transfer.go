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
	flags := os.O_WRONLY | os.O_CREATE
	if overwrite {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	remote, err := client.OpenFile(remotePath, flags)
	if err != nil {
		return TransferResult{}, fmt.Errorf("create remote file: %w", err)
	}
	defer remote.Close()

	hasher := sha256.New()
	reader := &progressReader{
		reader: io.TeeReader(local, hasher),
		total:  info.Size(),
		fn:     progress,
	}
	copied, err := io.Copy(remote, reader)
	if err != nil {
		return TransferResult{}, fmt.Errorf("upload file: %w", err)
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))
	if progress != nil {
		progress(copied, info.Size())
	}
	return TransferResult{
		Bytes:          copied,
		Size:           info.Size(),
		ChecksumSHA256: checksum,
		DurationMS:     time.Since(started).Milliseconds(),
	}, nil
}

func DownloadFile(ctx context.Context, target Target, remotePath string, localPath string, progress TransferProgress) (TransferResult, error) {
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

	hasher := sha256.New()
	writer := &progressWriter{
		writer: io.MultiWriter(local, hasher),
		total:  info.Size(),
		fn:     progress,
	}
	copied, err := io.Copy(writer, remote)
	if err != nil {
		return TransferResult{}, fmt.Errorf("download file: %w", err)
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))
	if progress != nil {
		progress(copied, info.Size())
	}
	return TransferResult{
		Bytes:          copied,
		Size:           info.Size(),
		ChecksumSHA256: checksum,
		DurationMS:     time.Since(started).Milliseconds(),
	}, nil
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
