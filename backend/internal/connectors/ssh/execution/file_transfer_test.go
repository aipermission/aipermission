package execution

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/pkg/sftp"
)

func TestCopyWithProgressStopsAtConfiguredByteLimit(t *testing.T) {
	var output bytes.Buffer
	written, _, err := copyWithProgress(context.Background(), &output, strings.NewReader("12345"), 5, TransferOptions{MaxBytes: 4})
	if err == nil || written != 0 || output.Len() != 0 {
		t.Fatalf("expected pre-write byte limit rejection: written=%d output=%q err=%v", written, output.String(), err)
	}
}

func TestSetupWithContextClosesConnectionWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	connection := &blockingSetupConnection{closed: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := setupWithContext(ctx, connection, func() (string, error) {
			<-connection.closed
			return "", errors.New("connection closed")
		})
		done <- err
	}()

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("setup error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled setup did not close its connection")
	}
}

type blockingSetupConnection struct {
	closed chan struct{}
}

func (connection *blockingSetupConnection) Close() error {
	select {
	case <-connection.closed:
	default:
		close(connection.closed)
	}
	return nil
}

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
	stagingDir, tempPath, err := remoteUploadTempPaths("/var/www/app.zip", 2)
	if err != nil {
		t.Fatal(err)
	}
	if path.Dir(stagingDir) != "/var/www" || path.Dir(tempPath) != stagingDir || !strings.HasPrefix(path.Base(stagingDir), ".aipermission-upload-") {
		t.Fatalf("temporary upload path should stay beside the target file, got %q", tempPath)
	}
	if !strings.HasSuffix(stagingDir, "-2") || path.Base(tempPath) != "payload.tmp" {
		t.Fatalf("temporary upload path should include the attempt suffix, got %q", tempPath)
	}
}

func TestCommitRemoteUploadWithoutOverwriteRejectsExistingTarget(t *testing.T) {
	client := &fakeUploadCommitter{existing: map[string]bool{"/tmp/app.zip": true}}

	err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", false)
	if err == nil || !strings.Contains(err.Error(), "remote file already exists") {
		t.Fatalf("expected existing target error, got %v", err)
	}
	if len(client.links) != 0 {
		t.Fatalf("link should not run when overwrite is disabled and target exists: %#v", client.links)
	}
}

func TestCommitRemoteUploadWithoutOverwriteRejectsRacingTarget(t *testing.T) {
	client := &fakeUploadCommitter{existing: map[string]bool{}, createTargetBeforeLink: true}

	err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", false)
	if err == nil || !strings.Contains(err.Error(), "remote file already exists") {
		t.Fatalf("racing target error = %v", err)
	}
	if !client.existing["/tmp/app.zip"] || len(client.removes) != 0 {
		t.Fatalf("racing target was changed: existing=%#v removes=%#v", client.existing, client.removes)
	}
}

func TestCommitRemoteUploadWithoutOverwriteFailsClosedWithoutHardlink(t *testing.T) {
	client := &fakeUploadCommitter{
		existing: map[string]bool{},
		linkErrs: []error{errors.New("unsupported extension: hardlink@openssh.com")},
	}

	err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", false)
	if connectors.ErrorCode(err) != "atomic_create_unsupported" || client.existing["/tmp/app.zip"] {
		t.Fatalf("unsupported hardlink result: err=%v existing=%#v", err, client.existing)
	}
}

func TestCommitRemoteUploadWithoutOverwritePublishesWithNoReplaceLink(t *testing.T) {
	client := &fakeUploadCommitter{existing: map[string]bool{}}

	if err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", false); err != nil {
		t.Fatalf("commit upload: %v", err)
	}
	if len(client.links) != 1 || client.links[0] != "/tmp/.aipermission-upload-app.zip.tmp -> /tmp/app.zip" || len(client.removes) != 1 {
		t.Fatalf("unexpected no-replace commit: links=%#v removes=%#v", client.links, client.removes)
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
	if len(client.chmods) != 1 || client.chmods[0] != "/tmp/.aipermission-upload-app.zip.tmp:0600" {
		t.Fatalf("overwrite should preserve destination permissions, chmods=%#v", client.chmods)
	}
}

func TestCommitRemoteUploadOverwritePreservesSpecialPermissionBits(t *testing.T) {
	const destination = "/tmp/app.zip"
	const staging = "/tmp/.aipermission-upload-app.zip.tmp"
	want := os.FileMode(0o755) | os.ModeSetuid | os.ModeSetgid | os.ModeSticky
	client := &fakeUploadCommitter{
		existing: map[string]bool{destination: true, staging: true},
		metadata: map[string]remoteFileMetadata{
			destination: {Mode: want, UID: 1000, GID: 1000},
			staging:     {Mode: 0o600, UID: 1000, GID: 1000},
		},
	}

	if err := commitRemoteUpload(client, staging, destination, true); err != nil {
		t.Fatalf("commit upload: %v", err)
	}
	if len(client.chmodModes) != 1 || client.chmodModes[0] != want {
		t.Fatalf("overwrite mode=%#o, want %#o", client.chmodModes[0], want)
	}
}

func TestCommitRemoteUploadRejectsOwnershipDriftBeforeOverwrite(t *testing.T) {
	client := &fakeUploadCommitter{
		existing: map[string]bool{
			"/tmp/app.zip":                          true,
			"/tmp/.aipermission-upload-app.zip.tmp": true,
		},
		fileStats: map[string]*sftp.FileStat{
			"/tmp/app.zip":                          {UID: 1000, GID: 1000},
			"/tmp/.aipermission-upload-app.zip.tmp": {UID: 1001, GID: 1000},
		},
	}

	err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", true)
	if err == nil || !strings.Contains(err.Error(), "staging owner differs") {
		t.Fatalf("ownership drift should fail closed: %v", err)
	}
	if len(client.posixRenames) != 0 {
		t.Fatalf("ownership drift reached commit: %#v", client.posixRenames)
	}
}

func TestCommitRemoteUploadFailsClosedWithoutCompleteDestinationMetadata(t *testing.T) {
	remotePath := "/tmp/app.zip"
	client := &fakeUploadCommitter{
		existing:       map[string]bool{remotePath: true},
		metadataErrors: map[string]error{remotePath: errors.New("SFTP attributes omitted")},
	}

	err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", remotePath, true)
	if err == nil || !strings.Contains(err.Error(), "preserve remote destination metadata") {
		t.Fatalf("incomplete metadata must fail closed: %v", err)
	}
	if len(client.posixRenames) != 0 || len(client.chmods) != 0 {
		t.Fatalf("incomplete metadata reached mutation: renames=%#v chmods=%#v", client.posixRenames, client.chmods)
	}
}

func TestParseRemoteFileMetadataPreservesRootOwnershipAndPermissions(t *testing.T) {
	metadata, err := parseRemoteFileMetadata("gnu 81c0 0 0\n")
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.Mode.IsRegular() || metadata.Mode.Perm() != 0o700 || metadata.UID != 0 || metadata.GID != 0 {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestParseRemoteFileMetadataAcceptsBSDStatMode(t *testing.T) {
	metadata, err := parseRemoteFileMetadata("bsd 100640 501 20\n")
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.Mode.IsRegular() || metadata.Mode.Perm() != 0o640 || metadata.UID != 501 || metadata.GID != 20 {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestRemoteMetadataCommandSupportsGNUAndBSDStatWithoutOptionLikePaths(t *testing.T) {
	command := remoteMetadataCommand("-private")
	if !strings.Contains(command, "stat -c") || !strings.Contains(command, "stat -f") || !strings.Contains(command, "'./-private'") {
		t.Fatalf("metadata command = %q", command)
	}
}

func TestQuoteRemoteShellArgEscapesPaths(t *testing.T) {
	if got := quoteRemoteShellArg("/tmp/user's file"); got != `'/tmp/user'\''s file'` {
		t.Fatalf("quoted path = %q", got)
	}
}

func TestCommitRemoteUploadOverwriteUsesNoReplaceLinkWhenTargetIsAbsent(t *testing.T) {
	client := &fakeUploadCommitter{existing: map[string]bool{}}

	if err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", true); err != nil {
		t.Fatalf("commit new upload with overwrite enabled: %v", err)
	}
	if len(client.links) != 1 || len(client.posixRenames) != 0 {
		t.Fatalf("new target should use no-replace publish: links=%#v posix=%#v", client.links, client.posixRenames)
	}
}

func TestCommitRemoteUploadOverwriteFailsClosedWithoutAtomicReplace(t *testing.T) {
	client := &fakeUploadCommitter{
		existing:       map[string]bool{"/tmp/app.zip": true},
		posixRenameErr: errors.New("unsupported extension: posix-rename@openssh.com"),
	}

	err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", true)
	if err == nil || connectors.ErrorCode(err) != "atomic_replace_unsupported" {
		t.Fatalf("expected atomic replace rejection, got %v", err)
	}
	if !client.existing["/tmp/app.zip"] {
		t.Fatal("failed overwrite removed the original destination")
	}
	if len(client.links) != 0 || len(client.removes) != 0 {
		t.Fatalf("unsupported atomic replace performed destructive fallback: links=%#v removes=%#v", client.links, client.removes)
	}
}

func TestCommitRemoteUploadOverwriteClassifiesLostReplyAsOutcomeUnknown(t *testing.T) {
	client := &fakeUploadCommitter{
		existing:       map[string]bool{"/tmp/app.zip": true},
		posixRenameErr: errors.New("connection reset after request dispatch"),
	}

	err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", true)
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		t.Fatalf("error status = %q, want outcome_unknown: %v", connectors.ErrorStatus(err), err)
	}
	details := connectors.ErrorDetails(err)
	if details["retry_safe"] != false || details["dispatch_stage"] != "posix_rename" {
		t.Fatalf("unexpected outcome details: %#v", details)
	}
	if len(client.links) != 0 || len(client.removes) != 0 {
		t.Fatalf("uncertain atomic replace performed fallback mutations: links=%#v removes=%#v", client.links, client.removes)
	}
}

func TestCommitRemoteUploadOverwriteKeepsPermissionFailureDefinite(t *testing.T) {
	client := &fakeUploadCommitter{
		existing:       map[string]bool{"/tmp/app.zip": true},
		posixRenameErr: os.ErrPermission,
	}

	err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", true)
	if err == nil || connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		t.Fatalf("permission rejection must remain a definite failure: %v", err)
	}
	if !client.existing["/tmp/app.zip"] {
		t.Fatal("permission rejection removed the original destination")
	}
}

func TestCommitRemoteUploadWithoutOverwriteClassifiesLostReplyAsOutcomeUnknown(t *testing.T) {
	client := &fakeUploadCommitter{
		existing: map[string]bool{},
		linkErrs: []error{errors.New("connection lost")},
	}

	err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", false)
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		t.Fatalf("error status = %q, want outcome_unknown: %v", connectors.ErrorStatus(err), err)
	}
}

func TestCommitRemoteUploadWithoutOverwriteKeepsCreatedTargetAmbiguousAfterLostReply(t *testing.T) {
	client := &fakeUploadCommitter{
		existing:                  map[string]bool{},
		createTargetWithLinkError: true,
		linkErrs:                  []error{errors.New("connection lost after link dispatch")},
	}

	err := commitRemoteUpload(client, "/tmp/.aipermission-upload-app.zip.tmp", "/tmp/app.zip", false)
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		t.Fatalf("error status = %q, want outcome_unknown: %v", connectors.ErrorStatus(err), err)
	}
	if !client.existing["/tmp/app.zip"] {
		t.Fatal("fixture did not publish target before losing the reply")
	}
}

func TestFailedRemoteUploadRetriesCleanupAndPreservesPrimaryError(t *testing.T) {
	primary := context.Canceled
	currentCalls := 0
	reconnectCalls := 0
	err, cleaned := failedRemoteUploadResult(primary, "/tmp/.aipermission-upload.tmp", func() error {
		currentCalls++
		return errors.New("closed connection")
	}, func() error {
		reconnectCalls++
		return nil
	})
	if !errors.Is(err, primary) || !cleaned || currentCalls != 1 || reconnectCalls != 1 {
		t.Fatalf("cleanup retry result = %v cleaned=%v current=%d reconnect=%d", err, cleaned, currentCalls, reconnectCalls)
	}
}

func TestFailedRemoteUploadExposesUnreconciledStagingAsOutcomeUnknown(t *testing.T) {
	tempPath := "/tmp/.aipermission-upload.tmp"
	err, cleaned := failedRemoteUploadResult(context.Canceled, tempPath,
		func() error { return errors.New("closed connection") },
		func() error { return errors.New("reconnect failed") },
	)
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || cleaned {
		t.Fatalf("cleanup failure status = %q, want outcome_unknown: %v", connectors.ErrorStatus(err), err)
	}
	details := connectors.ErrorDetails(err)
	if details["remote_staging_path"] != tempPath || details["retry_safe"] != false {
		t.Fatalf("cleanup failure details = %#v", details)
	}
}

func TestRemoteUploadCleanupBoundsBlockedRemove(t *testing.T) {
	release := make(chan struct{})
	client := blockingRemoveClient{started: make(chan struct{}), release: release}
	connection := &timeoutRecordingCloser{release: release}
	started := time.Now()
	err := removeRemoteUploadTempWithin(client, connection, "/tmp/.aipermission-upload-file.tmp", 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || !connection.closed || time.Since(started) > time.Second {
		t.Fatalf("bounded cleanup err=%v closed=%v elapsed=%v", err, connection.closed, time.Since(started))
	}
}

func TestRemoteUploadRecordsOwnedNamespaceBeforeCreatingPlaintext(t *testing.T) {
	sequence := []string{}
	client := &orderingRemoteUploadClient{
		onMkdir: func() { sequence = append(sequence, "mkdir") },
		onChmod: func() { sequence = append(sequence, "chmod") },
		onOpen:  func() { sequence = append(sequence, "open") },
	}
	path, _, recorded, err := createRemoteUploadTemp(t.Context(), client, "/tmp/file", TransferOptions{
		RecordStaging: func(context.Context, string) error {
			sequence = append(sequence, "record")
			return nil
		},
		ClearStaging: func(context.Context, string) error { return nil },
	})
	if err == nil || path == "" || !recorded {
		t.Fatalf("create result path=%q recorded=%v err=%v", path, recorded, err)
	}
	if strings.Join(sequence, ",") != "mkdir,chmod,record,open" {
		t.Fatalf("staging sequence = %v, plaintext creation must follow durable namespace ownership", sequence)
	}
}

func TestRemoteUploadCollisionDoesNotCreateRecoveryOwnership(t *testing.T) {
	recordCalls := 0
	client := &orderingRemoteUploadClient{mkdirErr: os.ErrExist}
	path, _, recorded, err := createRemoteUploadTemp(t.Context(), client, "/tmp/file", TransferOptions{
		RecordStaging: func(context.Context, string) error {
			recordCalls++
			return nil
		},
		ClearStaging: func(context.Context, string) error {
			t.Fatal("an unowned collision must not have a staging record to clear")
			return nil
		},
	})
	if err == nil || path != "" || recorded || recordCalls != 0 {
		t.Fatalf("collision result path=%q recorded=%v record_calls=%d err=%v", path, recorded, recordCalls, err)
	}
}

func TestRemoteUploadRequiresPairedStagingCallbacks(t *testing.T) {
	_, err := UploadFileWithOptions(t.Context(), Target{}, "/missing", "/remote", false, TransferOptions{
		RecordStaging: func(context.Context, string) error { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "callbacks must be configured together") {
		t.Fatalf("unpaired staging callbacks error = %v", err)
	}
}

func TestRemoteUploadStagingPathValidationRejectsArbitraryPaths(t *testing.T) {
	for _, value := range []string{"/tmp/file", "/tmp/.aipermission-upload-file.tmp/child", "/tmp/../etc/.aipermission-upload-file.tmp", "/tmp/.aipermission-upload-0123456789abcdef0123456789abcdef-0/other", ""} {
		if validRemoteUploadTempPath(value) {
			t.Fatalf("unsafe remote staging path accepted: %q", value)
		}
	}
	if !validRemoteUploadTempPath("/tmp/.aipermission-upload-0123456789abcdef0123456789abcdef-0/payload.tmp") {
		t.Fatal("generated remote staging path rejected")
	}
}

func TestRemoteUploadTempPathIsBoundedAndDoesNotExposeDestinationName(t *testing.T) {
	remotePath := "/tmp/" + strings.Repeat("private-", 80) + ".txt"
	stagingDir, tempPath, err := remoteUploadTempPaths(remotePath, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(path.Base(stagingDir)) > 128 || strings.Contains(tempPath, "private-") || path.Dir(stagingDir) != "/tmp" || path.Dir(tempPath) != stagingDir {
		t.Fatalf("unsafe temporary upload paths dir=%q file=%q", stagingDir, tempPath)
	}
}

type fakeUploadCommitter struct {
	existing                  map[string]bool
	fileStats                 map[string]*sftp.FileStat
	metadata                  map[string]remoteFileMetadata
	metadataErrors            map[string]error
	createTargetBeforeLink    bool
	createTargetWithLinkError bool
	posixRenameErr            error
	linkErrs                  []error
	links                     []string
	posixRenames              []string
	removes                   []string
	chmods                    []string
	chmodModes                []os.FileMode
}

type blockingRemoveClient struct {
	started chan struct{}
	release chan struct{}
}

func (client blockingRemoveClient) Remove(string) error {
	close(client.started)
	<-client.release
	return net.ErrClosed
}

func (blockingRemoveClient) RemoveDirectory(string) error { return nil }

type timeoutRecordingCloser struct {
	closed  bool
	release chan struct{}
}

type orderingRemoteUploadClient struct {
	onMkdir  func()
	onChmod  func()
	onOpen   func()
	mkdirErr error
	openErr  error
}

func (client *orderingRemoteUploadClient) Stat(string) (os.FileInfo, error) {
	return nil, os.ErrNotExist
}
func (client *orderingRemoteUploadClient) Mkdir(string) error {
	if client.onMkdir != nil {
		client.onMkdir()
	}
	return client.mkdirErr
}
func (client *orderingRemoteUploadClient) OpenFile(string, int) (*sftp.File, error) {
	if client.onOpen != nil {
		client.onOpen()
	}
	if client.openErr != nil {
		return nil, client.openErr
	}
	return nil, net.ErrClosed
}
func (client *orderingRemoteUploadClient) Chmod(string, os.FileMode) error {
	if client.onChmod != nil {
		client.onChmod()
	}
	return nil
}
func (*orderingRemoteUploadClient) Link(string, string) error        { return nil }
func (*orderingRemoteUploadClient) PosixRename(string, string) error { return nil }
func (*orderingRemoteUploadClient) Remove(string) error              { return nil }
func (*orderingRemoteUploadClient) RemoveDirectory(string) error     { return nil }

func (closer *timeoutRecordingCloser) Close() error {
	if !closer.closed {
		closer.closed = true
		close(closer.release)
	}
	return nil
}

func (f *fakeUploadCommitter) Lstat(path string) (os.FileInfo, error) {
	if f.existing[path] {
		return fakeFileInfo{name: filepath.Base(path), stat: f.fileStats[path]}, nil
	}
	if strings.Contains(path, ".aipermission-upload-") {
		return fakeFileInfo{name: filepath.Base(path), stat: f.fileStats[path]}, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeUploadCommitter) CompleteMetadata(path string) (remoteFileMetadata, error) {
	if err := f.metadataErrors[path]; err != nil {
		return remoteFileMetadata{}, err
	}
	if metadata, ok := f.metadata[path]; ok {
		return metadata, nil
	}
	if stat := f.fileStats[path]; stat != nil {
		return remoteFileMetadata{Mode: stat.FileMode(), UID: stat.UID, GID: stat.GID}, nil
	}
	return remoteFileMetadata{Mode: 0o600, UID: 1000, GID: 1000}, nil
}

func (f *fakeUploadCommitter) Link(oldname string, newname string) error {
	f.links = append(f.links, oldname+" -> "+newname)
	if f.createTargetBeforeLink {
		f.existing[newname] = true
	}
	if f.existing[newname] {
		return os.ErrExist
	}
	if len(f.linkErrs) == 0 {
		f.existing[oldname] = true
		f.existing[newname] = true
		return nil
	}
	err := f.linkErrs[0]
	f.linkErrs = f.linkErrs[1:]
	if err != nil {
		if f.createTargetWithLinkError {
			f.existing[oldname] = true
			f.existing[newname] = true
		}
		return err
	}
	f.existing[oldname] = true
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

func (f *fakeUploadCommitter) Chmod(path string, mode os.FileMode) error {
	f.chmods = append(f.chmods, fmt.Sprintf("%s:%04o", path, mode.Perm()))
	f.chmodModes = append(f.chmodModes, mode)
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
	stat *sftp.FileStat
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 1 }
func (f fakeFileInfo) Mode() os.FileMode  { return 0o600 }
func (f fakeFileInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return f.stat }
