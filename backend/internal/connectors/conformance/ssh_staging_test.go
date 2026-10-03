package conformance_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/pkg/sftp"
)

func assertSFTPContent(t *testing.T, files *sftp.Client, remote string, expected []byte) {
	t.Helper()
	file, err := files.Open(remote)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(len(expected)+1)))
	if err != nil || !bytes.Equal(data, expected) {
		t.Fatalf("SFTP destination changed before atomic commit: %x / %v", data, err)
	}
}

func assertSFTPStagingCommit(t *testing.T, ctx context.Context, target execution.Target, files *sftp.Client, local, remote string, original []byte, size int64) {
	t.Helper()
	staged := false
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err := execution.UploadFileWithOptions(canceled, target, local, remote, true, execution.TransferOptions{
		Progress: func(copied, _ int64) {
			if copied == size {
				staged = true
				assertSFTPContent(t, files, remote, original)
				cancel()
			}
		},
	})
	if !staged || !errors.Is(err, context.Canceled) {
		t.Fatalf("SFTP did not cancel a fully staged upload: staged=%v / %v", staged, err)
	}
	assertSFTPContent(t, files, remote, original)
	entries, err := execution.ListRemoteDirectory(ctx, target, filepath.Dir(remote))
	if err != nil || len(entries) != 1 || entries[0].Name != "payload.bin" {
		t.Fatalf("SFTP canceled staging was not cleaned: %#v / %v", entries, err)
	}
	staged = false
	_, err = execution.UploadFileWithOptions(ctx, target, local, remote, true, execution.TransferOptions{
		Progress: func(copied, _ int64) {
			if copied == size && !staged {
				staged = true
				assertSFTPContent(t, files, remote, original)
			}
		},
	})
	if err != nil || !staged {
		t.Fatalf("SFTP overwrite did not preserve old bytes through staging: %v", err)
	}
}
