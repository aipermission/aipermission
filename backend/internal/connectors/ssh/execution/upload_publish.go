package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func validateUploadSource(ctx context.Context, local *os.File, info os.FileInfo, expected *connectors.FileContentIdentity) error {
	if expected == nil {
		return nil
	}
	if err := expected.Validate(); err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != expected.SizeBytes {
		return connectors.ErrUploadContentChanged
	}
	size, digest, err := copyWithProgress(ctx, io.Discard, io.LimitReader(local, expected.SizeBytes+1), expected.SizeBytes, TransferOptions{})
	if err != nil {
		return err
	}
	if err := expected.Verify(size, digest); err != nil {
		return err
	}
	_, err = local.Seek(0, io.SeekStart)
	return err
}

func verifyUploadContent(expected *connectors.FileContentIdentity, size int64, checksum string) error {
	if expected == nil {
		return nil
	}
	return expected.Verify(size, checksum)
}

func checkUploadBeforeWriting(ctx context.Context, authorize connectors.FileUploadAuthorization) error {
	return withUploadAuthorization(ctx, authorize, func(context.Context) error { return nil })
}

func publishRemoteUpload(ctx context.Context, committer remoteUploadCommitter, connection io.Closer, staging, destination string, overwrite bool, authorize connectors.FileUploadAuthorization) error {
	if authorize == nil {
		return commitRemoteUpload(ctx, committer, staging, destination, overwrite)
	}
	return commitRemoteUploadWithGuard(ctx, committer, staging, destination, overwrite, func(mutate func() error) error {
		return withUploadAuthorization(ctx, authorize, func(authorized context.Context) error {
			stop := closeOnContext(authorized, connection)
			defer stop()
			return mutate()
		})
	})
}

func withUploadAuthorization(ctx context.Context, authorize connectors.FileUploadAuthorization, run func(context.Context) error) error {
	if authorize == nil {
		return run(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	authorized, release, err := authorize(ctx)
	if err != nil {
		return err
	}
	if authorized == nil || release == nil {
		return errors.New("upload publication authorization is unavailable")
	}
	defer release()
	if err := authorized.Err(); err != nil {
		return err
	}
	return run(authorized)
}

func replaceRemoteUpload(client remoteUploadCommitter, staging, destination string) error {
	err := client.PosixRename(staging, destination)
	if err == nil {
		return nil
	}
	if isUnsupportedPosixRename(err) {
		return connectors.ClassifyError("atomic_replace_unsupported",
			fmt.Errorf("remote server does not support safe atomic overwrite; keep overwrite disabled or remove the destination explicitly: %w", err))
	}
	return classifyRemoteRenameError("posix_rename", err)
}

func linkRemoteUpload(client remoteUploadCommitter, staging, destination string) error {
	err := client.Link(staging, destination)
	if err == nil {
		return nil
	}
	if isDefiniteRemoteFileExists(err) {
		return errors.New("remote file already exists")
	}
	if isUnsupportedHardlink(err) {
		return connectors.ClassifyError("atomic_create_unsupported",
			fmt.Errorf("remote server does not support safe atomic create; refusing a racy upload: %w", err))
	}
	return classifyRemoteRenameError("hardlink", err)
}
