//go:build !windows

package execution

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/pkg/sftp"
)

type localMetadataCommitter struct {
	*sftp.Client
}

func (client localMetadataCommitter) CompleteMetadata(remotePath string) (remoteFileMetadata, error) {
	info, err := os.Lstat(remotePath)
	if err != nil {
		return remoteFileMetadata{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return remoteFileMetadata{}, fmt.Errorf("local fixture stat type = %T", info.Sys())
	}
	return remoteFileMetadata{Mode: info.Mode(), UID: stat.Uid, GID: stat.Gid}, nil
}

func newSFTPFixture(t *testing.T) (*sftp.Client, string) {
	t.Helper()
	root := t.TempDir()
	serverConnection, clientConnection := net.Pipe()
	server, err := sftp.NewServer(serverConnection, sftp.WithServerWorkingDirectory(root))
	if err != nil {
		t.Fatalf("create SFTP server: %v", err)
	}
	go func() { _ = server.Serve() }()
	client, err := sftp.NewClientPipe(clientConnection, clientConnection)
	if err != nil {
		_ = server.Close()
		t.Fatalf("create SFTP client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return client, root
}

func TestListRemoteDirectoryResolvesHomePathsAndCanonicalEntries(t *testing.T) {
	client, root := newSFTPFixture(t)
	if err := os.WriteFile(filepath.Join(root, "visible.txt"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "child.txt"), []byte("child"), 0o600); err != nil {
		t.Fatal(err)
	}

	items, err := listRemoteDirectoryWithClient(client, "~")
	if err != nil {
		t.Fatalf("list default home: %v", err)
	}
	if len(items) != 2 || !filepath.IsAbs(items[0].Path) || !filepath.IsAbs(items[1].Path) {
		t.Fatalf("default home entries are not canonical absolute paths: %#v", items)
	}

	nested, err := listRemoteDirectoryWithClient(client, "~/nested")
	if err != nil {
		t.Fatalf("list home subdirectory: %v", err)
	}
	if len(nested) != 1 || nested[0].Name != "child.txt" || !filepath.IsAbs(nested[0].Path) {
		t.Fatalf("unexpected nested entries: %#v", nested)
	}
	if _, err := client.Stat(nested[0].Path); err != nil {
		t.Fatalf("returned entry path is not reusable: %v", err)
	}
}

func TestRemoteUploadStagingAndOverwritePreservePrivatePermissions(t *testing.T) {
	oldMask := syscall.Umask(0o022)
	defer syscall.Umask(oldMask)
	client, root := newSFTPFixture(t)
	destination := filepath.Join(root, "credentials")
	if err := os.WriteFile(destination, []byte("old-secret"), 0o700); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	beforeStat, ok := before.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("destination stat type = %T", before.Sys())
	}

	tempPath, remote, _, err := createRemoteUploadTemp(t.Context(), client, destination, TransferOptions{})
	if err != nil {
		t.Fatalf("create upload staging: %v", err)
	}
	stagingInfo, err := os.Stat(filepath.Dir(tempPath))
	if err != nil {
		t.Fatal(err)
	}
	if stagingInfo.Mode().Perm() != 0o700 {
		t.Fatalf("staging directory mode=%04o", stagingInfo.Mode().Perm())
	}
	tempInfo, err := os.Stat(tempPath)
	if err != nil {
		t.Fatal(err)
	}
	if tempInfo.Mode().Perm() != 0o600 {
		t.Fatalf("temporary payload mode=%04o", tempInfo.Mode().Perm())
	}
	if _, err := remote.Write([]byte("new-secret")); err != nil {
		t.Fatal(err)
	}
	if err := remote.Close(); err != nil {
		t.Fatal(err)
	}
	if err := commitRemoteUpload(localMetadataCommitter{Client: client}, tempPath, destination, true); err != nil {
		t.Fatalf("commit upload: %v", err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("overwrite mode=%04o, want preserved 0700", info.Mode().Perm())
	}
	afterStat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("overwritten destination stat type = %T", info.Sys())
	}
	if afterStat.Uid != beforeStat.Uid || afterStat.Gid != beforeStat.Gid {
		t.Fatalf("overwrite ownership=%d:%d, want preserved %d:%d", afterStat.Uid, afterStat.Gid, beforeStat.Uid, beforeStat.Gid)
	}
}
