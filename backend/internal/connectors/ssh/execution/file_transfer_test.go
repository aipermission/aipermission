package execution

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProgressReaderReportsTransferredBytes(t *testing.T) {
	var seenTransferred int64
	var seenTotal int64
	reader := &progressReader{
		reader: bytes.NewBufferString("hello"),
		total:  5,
		fn: func(transferred int64, total int64) {
			seenTransferred = transferred
			seenTotal = total
		},
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read progress reader: %v", err)
	}
	if string(data) != "hello" || seenTransferred != 5 || seenTotal != 5 {
		t.Fatalf("unexpected progress reader state data=%q transferred=%d total=%d", data, seenTransferred, seenTotal)
	}
}

func TestProgressWriterReportsTransferredBytes(t *testing.T) {
	var output bytes.Buffer
	var seenTransferred int64
	var seenTotal int64
	writer := &progressWriter{
		writer: &output,
		total:  5,
		fn: func(transferred int64, total int64) {
			seenTransferred = transferred
			seenTotal = total
		},
	}
	n, err := writer.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("write progress writer: %v", err)
	}
	if n != 5 || output.String() != "hello" || seenTransferred != 5 || seenTotal != 5 {
		t.Fatalf("unexpected progress writer state n=%d output=%q transferred=%d total=%d", n, output.String(), seenTransferred, seenTotal)
	}
}

func TestRemoteUploadTempPathUsesTargetDirectory(t *testing.T) {
	tempPath := remoteUploadTempPath("/var/www/app.zip", 2)
	if !strings.HasPrefix(tempPath, "/var/www/.aipermission-upload-app.zip-") {
		t.Fatalf("temporary upload path should stay beside the target file, got %q", tempPath)
	}
	if !strings.HasSuffix(tempPath, "-2.tmp") {
		t.Fatalf("temporary upload path should include the attempt suffix, got %q", tempPath)
	}
}

func TestCommitRemoteUploadWithoutOverwriteRejectsExistingTarget(t *testing.T) {
	client := &fakeUploadCommitter{existing: map[string]bool{"/tmp/app.zip": true}}

	err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", false)
	if err == nil || !strings.Contains(err.Error(), "remote file already exists") {
		t.Fatalf("expected existing target error, got %v", err)
	}
	if len(client.renames) != 0 {
		t.Fatalf("rename should not run when overwrite is disabled and target exists: %#v", client.renames)
	}
}

func TestCommitRemoteUploadWithoutOverwriteRenamesTemp(t *testing.T) {
	client := &fakeUploadCommitter{existing: map[string]bool{}}

	if err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", false); err != nil {
		t.Fatalf("commit upload: %v", err)
	}
	if len(client.renames) != 1 || client.renames[0] != "/tmp/.aipermission-upload-app.zip.tmp -> /tmp/app.zip" {
		t.Fatalf("unexpected rename operations: %#v", client.renames)
	}
}

func TestCommitRemoteUploadOverwriteUsesPosixRename(t *testing.T) {
	client := &fakeUploadCommitter{existing: map[string]bool{"/tmp/app.zip": true}}

	if err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", true); err != nil {
		t.Fatalf("commit upload: %v", err)
	}
	if len(client.posixRenames) != 1 || len(client.removes) != 0 {
		t.Fatalf("expected atomic posix rename without remove, posix=%#v removes=%#v", client.posixRenames, client.removes)
	}
}

func TestCommitRemoteUploadOverwriteFallsBackAfterCompletedUpload(t *testing.T) {
	client := &fakeUploadCommitter{
		existing:       map[string]bool{"/tmp/app.zip": true},
		posixRenameErr: errors.New("extension unsupported"),
		renameErrs:     []error{errors.New("target exists"), nil},
	}

	if err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", true); err != nil {
		t.Fatalf("commit upload: %v", err)
	}
	if len(client.removes) != 1 || client.removes[0] != "/tmp/app.zip" {
		t.Fatalf("expected existing target removal after completed upload, got %#v", client.removes)
	}
	if len(client.renames) != 2 {
		t.Fatalf("expected regular rename fallback attempts, got %#v", client.renames)
	}
}

type fakeUploadCommitter struct {
	existing       map[string]bool
	posixRenameErr error
	renameErrs     []error
	renames        []string
	posixRenames   []string
	removes        []string
}

func (f *fakeUploadCommitter) Stat(path string) (os.FileInfo, error) {
	if f.existing[path] {
		return fakeFileInfo{name: filepath.Base(path)}, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeUploadCommitter) Rename(oldname string, newname string) error {
	f.renames = append(f.renames, oldname+" -> "+newname)
	if len(f.renameErrs) == 0 {
		delete(f.existing, oldname)
		f.existing[newname] = true
		return nil
	}
	err := f.renameErrs[0]
	f.renameErrs = f.renameErrs[1:]
	if err != nil {
		return err
	}
	delete(f.existing, oldname)
	f.existing[newname] = true
	return nil
}

func (f *fakeUploadCommitter) PosixRename(oldname string, newname string) error {
	f.posixRenames = append(f.posixRenames, oldname+" -> "+newname)
	if f.posixRenameErr != nil {
		return f.posixRenameErr
	}
	delete(f.existing, oldname)
	f.existing[newname] = true
	return nil
}

func (f *fakeUploadCommitter) Remove(path string) error {
	f.removes = append(f.removes, path)
	if !f.existing[path] {
		return os.ErrNotExist
	}
	delete(f.existing, path)
	return nil
}

type fakeFileInfo struct {
	name string
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 1 }
func (f fakeFileInfo) Mode() os.FileMode  { return 0o600 }
func (f fakeFileInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }
