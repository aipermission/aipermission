package execution

import (
	"context"
	"fmt"
	"os"
	"time"
)

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
	stopContextClose := closeOnContext(ctx, sshClient)
	defer stopContextClose()

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
	if options.MaxBytes > 0 && info.Size() > options.MaxBytes {
		return TransferResult{}, fmt.Errorf("remote file is larger than %d bytes", options.MaxBytes)
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
