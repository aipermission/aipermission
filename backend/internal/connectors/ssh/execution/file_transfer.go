package execution

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const remoteUploadCleanupTimeout = 10 * time.Second

type TransferProgress = connectors.TransferProgress
type TransferOptions = connectors.TransferOptions
type TransferResult = connectors.TransferResult
type RemoteFileEntry = connectors.RemoteFileEntry
type RemotePathStatus = connectors.RemotePathStatus

func StatRemotePath(ctx context.Context, target Target, remotePath string) (RemotePathStatus, error) {
	client, sshClient, err := sftpClient(ctx, target)
	if err != nil {
		return RemotePathStatus{}, err
	}
	defer sshClient.Close()
	defer client.Close()
	stopContextClose := closeOnContext(ctx, sshClient)
	defer stopContextClose()

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
	stopContextClose := closeOnContext(ctx, sshClient)
	defer stopContextClose()

	return listRemoteDirectoryWithClient(client, remotePath)
}

type remoteDirectoryClient interface {
	RealPath(string) (string, error)
	ReadDir(string) ([]os.FileInfo, error)
}

func listRemoteDirectoryWithClient(client remoteDirectoryClient, remotePath string) ([]RemoteFileEntry, error) {
	resolvedPath, err := resolveRemoteDirectoryPath(client, remotePath)
	if err != nil {
		return nil, err
	}
	entries, err := client.ReadDir(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("read remote directory: %w", err)
	}
	items := make([]RemoteFileEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." {
			continue
		}
		entryPath := path.Join(resolvedPath, name)
		if resolvedPath == "/" {
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

func resolveRemoteDirectoryPath(client remoteDirectoryClient, remotePath string) (string, error) {
	if remotePath == "" || remotePath == "~" {
		remotePath = "."
	} else if strings.HasPrefix(remotePath, "~/") {
		home, err := client.RealPath(".")
		if err != nil {
			return "", fmt.Errorf("resolve remote home directory: %w", err)
		}
		remotePath = path.Join(home, strings.TrimPrefix(remotePath, "~/"))
	} else if strings.HasPrefix(remotePath, "~") {
		return "", fmt.Errorf("remote home paths must use ~ or ~/<path>")
	}
	resolved, err := client.RealPath(remotePath)
	if err != nil {
		return "", fmt.Errorf("resolve remote directory: %w", err)
	}
	if !path.IsAbs(resolved) {
		return "", fmt.Errorf("resolve remote directory: server returned a non-absolute path")
	}
	return path.Clean(resolved), nil
}

func UploadFile(ctx context.Context, target Target, localPath string, remotePath string, overwrite bool, progress TransferProgress) (TransferResult, error) {
	return UploadFileWithOptions(ctx, target, localPath, remotePath, overwrite, TransferOptions{Progress: progress})
}

func UploadFileWithOptions(ctx context.Context, target Target, localPath string, remotePath string, overwrite bool, options TransferOptions) (result TransferResult, returnErr error) {
	started := time.Now()
	if (options.RecordStaging == nil) != (options.ClearStaging == nil) {
		return TransferResult{}, fmt.Errorf("remote staging callbacks must be configured together")
	}
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
	stopContextClose := closeOnContext(ctx, sshClient)
	defer stopContextClose()

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

	tempPath, remote, stagingRecorded, err := createRemoteUploadTemp(ctx, client, remotePath, options)
	if err != nil {
		if remote != nil {
			_ = remote.Close()
		}
		if tempPath == "" {
			return TransferResult{}, err
		}
		returnErr, cleaned := failedRemoteUploadResult(err, tempPath,
			func() error {
				return removeRemoteUploadTempWithin(client, sshClient, tempPath, remoteUploadCleanupTimeout)
			},
			func() error { return cleanupRemoteUploadTemp(target, tempPath) },
		)
		if cleaned && stagingRecorded && options.ClearStaging != nil {
			if clearErr := options.ClearStaging(context.Background(), tempPath); clearErr != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("clear remote staging record: %w", clearErr))
			}
		}
		return TransferResult{}, returnErr
	}
	committed := false
	defer func() {
		if !committed {
			var cleaned bool
			returnErr, cleaned = failedRemoteUploadResult(returnErr, tempPath,
				func() error {
					return removeRemoteUploadTempWithin(client, sshClient, tempPath, remoteUploadCleanupTimeout)
				},
				func() error { return cleanupRemoteUploadTemp(target, tempPath) },
			)
			if cleaned && stagingRecorded && options.ClearStaging != nil {
				if err := options.ClearStaging(context.Background(), tempPath); err != nil {
					returnErr = errors.Join(returnErr, fmt.Errorf("clear remote staging record: %w", err))
				}
			}
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
	committer := &authenticatedUploadCommitter{Client: client, ssh: sshClient}
	if err := commitRemoteUpload(committer, tempPath, remotePath, overwrite); err != nil {
		return TransferResult{
			Bytes: copied, Size: info.Size(), ChecksumSHA256: checksum,
			DurationMS: time.Since(started).Milliseconds(),
		}, err
	}
	committed = true
	if err := removeRemoteUploadStagingDir(client, tempPath); err != nil {
		return TransferResult{
				Bytes: copied, Size: info.Size(), ChecksumSHA256: checksum,
				DurationMS: time.Since(started).Milliseconds(),
			}, connectors.ClassifyOutcomeUnknown("remote_staging_cleanup", map[string]any{
				"remote_staging_path": tempPath,
				"recovery_hint":       "The destination is committed; remove the empty remote staging directory before retrying.",
			}, fmt.Errorf("remove committed remote staging directory: %w", err))
	}
	if stagingRecorded && options.ClearStaging != nil {
		if err := options.ClearStaging(context.Background(), tempPath); err != nil {
			return TransferResult{
					Bytes: copied, Size: info.Size(), ChecksumSHA256: checksum,
					DurationMS: time.Since(started).Milliseconds(),
				}, connectors.ClassifyOutcomeUnknown("remote_staging_record", map[string]any{
					"remote_staging_path": tempPath,
					"recovery_hint":       "Inspect the destination before retrying the upload.",
				}, fmt.Errorf("clear committed remote staging record: %w", err))
		}
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

type remoteUploadClient interface {
	Stat(string) (os.FileInfo, error)
	Mkdir(string) error
	OpenFile(string, int) (*sftp.File, error)
	Chmod(string, os.FileMode) error
	Link(string, string) error
	PosixRename(string, string) error
	Remove(string) error
	RemoveDirectory(string) error
}

type remoteUploadCommitter interface {
	Lstat(string) (os.FileInfo, error)
	CompleteMetadata(string) (remoteFileMetadata, error)
	Chmod(string, os.FileMode) error
	Link(string, string) error
	PosixRename(string, string) error
	Remove(string) error
}

type remoteFileMetadata struct {
	Mode os.FileMode
	UID  uint32
	GID  uint32
}

type authenticatedUploadCommitter struct {
	*sftp.Client
	ssh *ssh.Client
}

func (client *authenticatedUploadCommitter) CompleteMetadata(remotePath string) (remoteFileMetadata, error) {
	if client == nil || client.ssh == nil {
		return remoteFileMetadata{}, fmt.Errorf("complete remote metadata is unavailable")
	}
	session, err := client.ssh.NewSession()
	if err != nil {
		return remoteFileMetadata{}, fmt.Errorf("open remote metadata session: %w", err)
	}
	defer session.Close()
	command := remoteMetadataCommand(remotePath)
	output, err := session.Output(command)
	if err != nil {
		return remoteFileMetadata{}, fmt.Errorf("read complete remote metadata: %w", err)
	}
	return parseRemoteFileMetadata(string(output))
}

func parseRemoteFileMetadata(output string) (remoteFileMetadata, error) {
	fields := strings.Fields(output)
	if len(fields) != 4 {
		return remoteFileMetadata{}, fmt.Errorf("read complete remote metadata: unexpected stat response")
	}
	modeBase := 0
	switch fields[0] {
	case "gnu":
		modeBase = 16
	case "bsd":
		modeBase = 8
	default:
		return remoteFileMetadata{}, fmt.Errorf("read complete remote metadata: unknown stat response")
	}
	rawMode, err := strconv.ParseUint(fields[1], modeBase, 32)
	if err != nil {
		return remoteFileMetadata{}, fmt.Errorf("read complete remote metadata mode: %w", err)
	}
	uid, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil {
		return remoteFileMetadata{}, fmt.Errorf("read complete remote metadata owner: %w", err)
	}
	gid, err := strconv.ParseUint(fields[3], 10, 32)
	if err != nil {
		return remoteFileMetadata{}, fmt.Errorf("read complete remote metadata group: %w", err)
	}
	mode := (&sftp.FileStat{Mode: uint32(rawMode)}).FileMode()
	return remoteFileMetadata{Mode: mode, UID: uint32(uid), GID: uint32(gid)}, nil
}

func remoteMetadataCommand(remotePath string) string {
	if strings.HasPrefix(remotePath, "-") {
		remotePath = "./" + remotePath
	}
	quotedPath := quoteRemoteShellArg(remotePath)
	return "if aip_stat=$(LC_ALL=C stat -c '%f %u %g' " + quotedPath + " 2>/dev/null); then " +
		"printf 'gnu %s\\n' \"$aip_stat\"; " +
		"elif aip_stat=$(LC_ALL=C stat -f '%p %u %g' " + quotedPath + " 2>/dev/null); then " +
		"printf 'bsd %s\\n' \"$aip_stat\"; else exit 1; fi"
}

func quoteRemoteShellArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func createRemoteUploadTemp(ctx context.Context, client remoteUploadClient, remotePath string, options TransferOptions) (string, *sftp.File, bool, error) {
	for attempt := 0; attempt < 10; attempt++ {
		stagingDir, tempPath, err := remoteUploadTempPaths(remotePath, attempt)
		if err != nil {
			return "", nil, false, err
		}
		if err := client.Mkdir(stagingDir); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", nil, false, fmt.Errorf("create owned remote staging directory: %w", err)
		}
		if err := client.Chmod(stagingDir, 0o700); err != nil {
			cleanupErr := client.RemoveDirectory(stagingDir)
			if cleanupErr != nil && !os.IsNotExist(cleanupErr) {
				err = errors.Join(err, fmt.Errorf("remove insecure staging directory: %w", cleanupErr))
			}
			return "", nil, false, fmt.Errorf("secure remote staging directory: %w", err)
		}
		recorded := false
		if options.RecordStaging != nil {
			if err := options.RecordStaging(ctx, tempPath); err != nil {
				cleanupErr := client.RemoveDirectory(stagingDir)
				if cleanupErr != nil && !os.IsNotExist(cleanupErr) {
					err = errors.Join(err, fmt.Errorf("remove unrecorded staging directory: %w", cleanupErr))
				}
				return "", nil, false, fmt.Errorf("record owned remote upload staging: %w", err)
			}
			recorded = true
		}
		remote, err := client.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if err != nil {
			return tempPath, nil, recorded, fmt.Errorf("create remote temporary file: %w", err)
		}
		if err := client.Chmod(tempPath, 0o600); err != nil {
			closeErr := remote.Close()
			if closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close insecure remote temporary file: %w", closeErr))
			}
			return tempPath, nil, recorded, fmt.Errorf("secure remote temporary file: %w", err)
		}
		return tempPath, remote, recorded, nil
	}
	return "", nil, false, fmt.Errorf("create remote staging directory: exhausted unique names")
}

func remoteUploadTempPaths(remotePath string, attempt int) (string, string, error) {
	dir := path.Dir(remotePath)
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", "", fmt.Errorf("create remote temporary file identity: %w", err)
	}
	name := fmt.Sprintf(".aipermission-upload-%s-%d", hex.EncodeToString(nonce), attempt)
	stagingDir := name
	if dir == "." || dir == "" {
		return stagingDir, path.Join(stagingDir, "payload.tmp"), nil
	}
	stagingDir = path.Join(dir, name)
	return stagingDir, path.Join(stagingDir, "payload.tmp"), nil
}

func commitRemoteUpload(client remoteUploadCommitter, tempPath string, remotePath string, overwrite bool) error {
	_, statErr := client.Lstat(remotePath)
	if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("stat remote file before commit: %w", statErr)
	}
	if statErr == nil {
		if !overwrite {
			return fmt.Errorf("remote file already exists")
		}
		existingMetadata, err := client.CompleteMetadata(remotePath)
		if err != nil {
			return fmt.Errorf("preserve remote destination metadata: %w", err)
		}
		if !existingMetadata.Mode.IsRegular() {
			return fmt.Errorf("remote destination is not a regular file")
		}
		stagedMetadata, err := client.CompleteMetadata(tempPath)
		if err != nil {
			return fmt.Errorf("preserve remote staging metadata: %w", err)
		}
		if err := requireMatchingRemoteOwnership(existingMetadata, stagedMetadata); err != nil {
			return err
		}
		if err := client.Chmod(tempPath, preservedPermissionMode(existingMetadata.Mode)); err != nil {
			return fmt.Errorf("preserve remote destination permissions: %w", err)
		}
		if err := client.PosixRename(tempPath, remotePath); err == nil {
			return nil
		} else if isUnsupportedPosixRename(err) {
			return connectors.ClassifyError(
				"atomic_replace_unsupported",
				fmt.Errorf("remote server does not support safe atomic overwrite; keep overwrite disabled or remove the destination explicitly: %w", err),
			)
		} else {
			return classifyRemoteRenameError("posix_rename", err)
		}
	}
	if err := client.Link(tempPath, remotePath); err != nil {
		if isDefiniteRemoteFileExists(err) {
			return fmt.Errorf("remote file already exists")
		}
		if isUnsupportedHardlink(err) {
			return connectors.ClassifyError(
				"atomic_create_unsupported",
				fmt.Errorf("remote server does not support safe atomic create; refusing a racy upload: %w", err),
			)
		}
		return classifyRemoteRenameError("hardlink", err)
	}
	if err := client.Remove(tempPath); err != nil {
		return connectors.ClassifyOutcomeUnknown("hardlink_cleanup", map[string]any{
			"recovery_hint": "The destination was created; remove the remote staging link after inspection.",
		}, fmt.Errorf("remove committed remote staging link: %w", err))
	}
	return nil
}

func preservedPermissionMode(mode os.FileMode) os.FileMode {
	return mode.Perm() | mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky)
}

func requireMatchingRemoteOwnership(existing remoteFileMetadata, staged remoteFileMetadata) error {
	if existing.UID != staged.UID || existing.GID != staged.GID {
		return fmt.Errorf("preserve remote destination ownership: staging owner differs from destination")
	}
	return nil
}

func isDefiniteRemoteFileExists(err error) bool {
	if errors.Is(err, os.ErrExist) {
		return true
	}
	var status *sftp.StatusError
	// SSH_FX_FILE_ALREADY_EXISTS is defined by SFTP v4+ but is not exported by pkg/sftp.
	return errors.As(err, &status) && status.Code == 11
}

type remoteUploadCleaner interface {
	Remove(string) error
	RemoveDirectory(string) error
}

func removeRemoteUploadTemp(client remoteUploadCleaner, tempPath string) error {
	if err := client.Remove(tempPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return removeRemoteUploadStagingDir(client, tempPath)
}

func removeRemoteUploadStagingDir(client interface{ RemoveDirectory(string) error }, tempPath string) error {
	if err := client.RemoveDirectory(path.Dir(tempPath)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func removeRemoteUploadTempWithin(client remoteUploadCleaner, connection interface{ Close() error }, tempPath string, timeout time.Duration) error {
	result := make(chan error, 1)
	go func() { result <- removeRemoteUploadTemp(client, tempPath) }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		_ = connection.Close()
		return context.DeadlineExceeded
	}
}

func failedRemoteUploadResult(primary error, tempPath string, removeCurrent, removeAfterReconnect func() error) (error, bool) {
	cleanupErr := removeCurrent()
	if cleanupErr == nil {
		return primary, true
	}
	if retryErr := removeAfterReconnect(); retryErr == nil {
		return primary, true
	} else {
		cleanupErr = errors.Join(cleanupErr, retryErr)
	}
	return connectors.ClassifyOutcomeUnknown("remote_staging_cleanup", map[string]any{
		"remote_staging_path": tempPath,
		"recovery_hint":       "Remove the remote staging file after inspecting the destination.",
	}, errors.Join(primary, fmt.Errorf("clean remote upload staging: %w", cleanupErr))), false
}

func cleanupRemoteUploadTemp(target Target, tempPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), remoteUploadCleanupTimeout)
	defer cancel()
	return CleanupRemoteUploadStaging(ctx, target, tempPath)
}

func CleanupRemoteUploadStaging(ctx context.Context, target Target, tempPath string) error {
	if !validRemoteUploadTempPath(tempPath) {
		return fmt.Errorf("invalid remote upload staging path")
	}
	client, sshClient, err := sftpClient(ctx, target)
	if err != nil {
		return fmt.Errorf("reconnect for remote staging cleanup: %w", err)
	}
	defer sshClient.Close()
	defer client.Close()
	stopContextClose := closeOnContext(ctx, sshClient)
	defer stopContextClose()
	if err := removeRemoteUploadTemp(client, tempPath); err != nil {
		return fmt.Errorf("remove remote staging after reconnect: %w", err)
	}
	return nil
}

func validRemoteUploadTempPath(value string) bool {
	cleaned := path.Clean(strings.TrimSpace(value))
	return cleaned == strings.TrimSpace(value) && path.Base(cleaned) == "payload.tmp" &&
		remoteUploadTempDirPattern.MatchString(path.Base(path.Dir(cleaned)))
}

var remoteUploadTempDirPattern = regexp.MustCompile(`^\.aipermission-upload-[a-f0-9]{32}-[0-9]+$`)

func classifyRemoteRenameError(stage string, err error) error {
	wrapped := fmt.Errorf("move uploaded file into place: %w", err)
	if isDefiniteSFTPMutationFailure(err) {
		return wrapped
	}
	return connectors.ClassifyOutcomeUnknown(stage, map[string]any{
		"recovery_hint": "Inspect the destination before retrying the upload.",
	}, wrapped)
}

func isUnsupportedPosixRename(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(strings.ToLower(err.Error()), "unsupported extension: posix-rename@openssh.com") {
		return true
	}
	var status *sftp.StatusError
	return errors.As(err, &status) && status.FxCode() == sftp.ErrSSHFxOpUnsupported
}

func isUnsupportedHardlink(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(strings.ToLower(err.Error()), "unsupported extension: hardlink@openssh.com") {
		return true
	}
	var status *sftp.StatusError
	return errors.As(err, &status) && status.FxCode() == sftp.ErrSSHFxOpUnsupported
}

func isDefiniteSFTPMutationFailure(err error) bool {
	if errors.Is(err, os.ErrPermission) || isUnsupportedPosixRename(err) || isUnsupportedHardlink(err) {
		return true
	}
	var status *sftp.StatusError
	if !errors.As(err, &status) {
		return false
	}
	return status.FxCode() == sftp.ErrSSHFxPermissionDenied || status.FxCode() == sftp.ErrSSHFxNoSuchFile
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
			if options.MaxBytes > 0 && copied+int64(nr) > options.MaxBytes {
				return copied, "", fmt.Errorf("transfer exceeds the %d byte limit", options.MaxBytes)
			}
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

func sftpClient(ctx context.Context, target Target) (*sftp.Client, *ssh.Client, error) {
	sshClient, err := DialSSH(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	client, err := setupWithContext(ctx, sshClient, func() (*sftp.Client, error) {
		return sftp.NewClient(sshClient)
	})
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, fmt.Errorf("start sftp client: %w", err)
	}
	return client, sshClient, nil
}

func setupWithContext[T any](ctx context.Context, connection interface{ Close() error }, setup func() (T, error)) (T, error) {
	var zero T
	if ctx == nil {
		ctx = context.Background()
	}
	stopContextClose := closeOnContext(ctx, connection)
	value, err := setup()
	stopped := stopContextClose()
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return zero, contextErr
		}
		return zero, err
	}
	if !stopped || ctx.Err() != nil {
		_ = connection.Close()
		if contextErr := ctx.Err(); contextErr != nil {
			return zero, contextErr
		}
		return zero, context.Canceled
	}
	return value, nil
}

func closeOnContext(ctx context.Context, closer interface{ Close() error }) func() bool {
	if ctx == nil || ctx.Done() == nil {
		return func() bool { return true }
	}
	return context.AfterFunc(ctx, func() { _ = closer.Close() })
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
