package filetransfer

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/aipermission/aipermission/backend/internal/archivepath"
	"github.com/aipermission/aipermission/backend/internal/localfilename"
)

func TestStageUploadAndReserveDownload(t *testing.T) {
	root := t.TempDir()
	payload := []byte("staged upload")
	staged, size, checksum, err := StageUpload(root, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("stage upload: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(staged) })
	wantDigest := sha256.Sum256(payload)
	if size != int64(len(payload)) || checksum != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("staged metadata = size %d checksum %q", size, checksum)
	}
	if contents, err := os.ReadFile(staged); err != nil || !bytes.Equal(contents, payload) {
		t.Fatalf("staged contents = %q, %v", contents, err)
	}

	reserved, err := ReserveDownload(root)
	if err != nil {
		t.Fatalf("reserve download: %v", err)
	}
	if !TempPathAllowed(root, reserved) {
		t.Fatalf("reserved path escaped root: %q", reserved)
	}
	CleanupPaths([]string{"", staged, reserved})
	for _, name := range []string{staged, reserved} {
		if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cleanup left %q: %v", name, err)
		}
	}
}

func TestStageUploadRemovesPartialFileAfterReadFailure(t *testing.T) {
	root := t.TempDir()
	_, _, _, err := StageUpload(root, failingReader{})
	if err == nil || !strings.Contains(err.Error(), "stage upload file") {
		t.Fatalf("stage failure = %v", err)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("partial upload remained: entries=%v err=%v", entries, readErr)
	}

	missingRoot := filepath.Join(root, "missing")
	if _, _, _, err := StageUpload(missingRoot, bytes.NewReader(nil)); err == nil {
		t.Fatal("expected staging under a missing root to fail")
	}
	if _, err := ReserveDownload(missingRoot); err == nil {
		t.Fatal("expected reserving under a missing root to fail")
	}
}

func TestCreateDownloadArchivePreservesHierarchyAndCompressedBytes(t *testing.T) {
	root := t.TempDir()
	nestedZip := testZipBytes(t)
	first := writeTransferFixture(t, root, "first.bin", nestedZip)
	second := writeTransferFixture(t, root, "second.txt", []byte("second"))
	third := writeTransferFixture(t, root, "third.txt", []byte("third"))

	archivePath, err := CreateDownloadArchive(root, BatchRecord{Items: []Record{
		{Status: StatusCompleted, TempPath: first, FileName: "first.zip", RemotePath: "/reports/first.zip"},
		{Status: StatusCompleted, TempPath: second, FileName: "same.txt", RemotePath: "/reports/nested/same.txt"},
		{Status: StatusCompleted, TempPath: third, FileName: "same.txt", RemotePath: "/reports/nested/same.txt"},
		{Status: StatusFailed, TempPath: filepath.Join(root, "ignored"), FileName: "ignored.txt", RemotePath: "/reports/ignored.txt"},
	}})
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(archivePath) })

	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer archive.Close()
	if len(archive.File) != 3 {
		t.Fatalf("archive entry count = %d", len(archive.File))
	}
	wantNames := []string{"first.zip", "nested/same.txt", "nested/same-2.txt"}
	for index, file := range archive.File {
		if file.Name != wantNames[index] {
			t.Fatalf("archive entry %d = %q, want %q", index, file.Name, wantNames[index])
		}
	}
	if archive.File[0].Method != zip.Store {
		t.Fatalf("nested zip method = %d, want Store", archive.File[0].Method)
	}
	reader, err := archive.File[0].Open()
	if err != nil {
		t.Fatalf("open nested archive: %v", err)
	}
	contents, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(contents, nestedZip) {
		t.Fatalf("nested archive changed: bytes=%d err=%v", len(contents), err)
	}
}

func TestCreateDownloadArchiveUsesCrossPlatformSafeEntryNames(t *testing.T) {
	root := t.TempDir()
	items := []Record{}
	for index, remotePath := range []string{
		"/reports/nested/.. /escape.txt",
		"/reports/CON.txt",
		"/reports/name. ",
		"/reports/Report.txt",
		"/reports/report.txt",
		"/reports/café.txt",
		"/reports/café.txt",
	} {
		tempPath := writeTransferFixture(t, root, fmt.Sprintf("source-%d", index), []byte(remotePath))
		items = append(items, Record{Status: StatusCompleted, TempPath: tempPath, RemotePath: remotePath})
	}

	archivePath, err := CreateDownloadArchive(root, BatchRecord{Items: items})
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(archivePath) })
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer archive.Close()
	want := []string{"nested/___/escape.txt", "_CON.txt", "name__", "Report.txt", "report-2.txt", "café.txt", "café-2.txt"}
	for index, file := range archive.File {
		if file.Name != want[index] {
			t.Fatalf("archive entry %d = %q, want %q", index, file.Name, want[index])
		}
	}
}

func TestCreateDownloadArchiveKeepsFinalNamesUniqueAtTheLengthBoundary(t *testing.T) {
	root := t.TempDir()
	firstName := "CON." + strings.Repeat("x", 156)
	secondName := "_CON." + strings.Repeat("x", 155)
	caseName := strings.Repeat("A", 160)
	items := []Record{}
	for index, name := range []string{
		firstName,
		secondName,
		caseName,
		strings.ToLower(caseName),
		strings.Repeat("界", localfilename.MaxRunes),
		strings.Repeat("界", localfilename.MaxRunes),
		strings.Repeat("🙂", localfilename.MaxRunes),
		strings.Repeat("🙂", localfilename.MaxRunes),
	} {
		tempPath := writeTransferFixture(t, root, fmt.Sprintf("long-%d", index), []byte(name))
		items = append(items, Record{Status: StatusCompleted, TempPath: tempPath, RemotePath: "/" + name})
	}

	archivePath, err := CreateDownloadArchive(root, BatchRecord{Items: items})
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(archivePath) })
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer archive.Close()
	seen := map[string]bool{}
	for _, file := range archive.File {
		if len([]rune(file.Name)) > localfilename.MaxRunes || len([]byte(file.Name)) > localfilename.MaxUTF8Bytes || len(utf16.Encode([]rune(file.Name))) > localfilename.MaxUTF16Units {
			t.Fatalf("archive name exceeds portable limits: %q", file.Name)
		}
		key := archivepath.CanonicalKey(file.Name)
		if seen[key] {
			t.Fatalf("archive contains colliding final name %q", file.Name)
		}
		seen[key] = true
	}
}

func TestCreateDownloadArchiveSeparatesFileDirectoryPrefixCollisions(t *testing.T) {
	for _, paths := range [][]string{
		{"/reports/CON", "/reports/_CON/child.txt"},
		{"/reports/_CON/child.txt", "/reports/CON"},
		{"/reports/Caf\u00e9", "/reports/cafe\u0301/child.txt"},
	} {
		t.Run(strings.Join(paths, "-"), func(t *testing.T) {
			root := t.TempDir()
			items := make([]Record, 0, len(paths))
			for index, remotePath := range paths {
				tempPath := writeTransferFixture(t, root, fmt.Sprintf("prefix-%d", index), []byte(remotePath))
				items = append(items, Record{Status: StatusCompleted, TempPath: tempPath, RemotePath: remotePath})
			}
			archivePath, err := CreateDownloadArchive(root, BatchRecord{Items: items})
			if err != nil {
				t.Fatalf("create archive: %v", err)
			}
			archive, err := zip.OpenReader(archivePath)
			if err != nil {
				t.Fatalf("open archive: %v", err)
			}
			defer archive.Close()
			files := map[string]bool{}
			for _, file := range archive.File {
				parts := strings.Split(file.Name, "/")
				for index := 1; index < len(parts); index++ {
					if files[archivepath.CanonicalKey(strings.Join(parts[:index], "/"))] {
						t.Fatalf("entry %q has a file as its parent", file.Name)
					}
				}
				files[archivepath.CanonicalKey(file.Name)] = true
			}
			extractRoot := t.TempDir()
			for _, file := range archive.File {
				target := filepath.Join(extractRoot, filepath.FromSlash(file.Name))
				if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
					t.Fatalf("create extraction parent for %q: %v", file.Name, err)
				}
				reader, err := file.Open()
				if err != nil {
					t.Fatalf("open archive entry %q: %v", file.Name, err)
				}
				contents, readErr := io.ReadAll(reader)
				closeErr := reader.Close()
				if readErr != nil || closeErr != nil {
					t.Fatalf("read archive entry %q: read=%v close=%v", file.Name, readErr, closeErr)
				}
				if err := os.WriteFile(target, contents, 0o600); err != nil {
					t.Fatalf("extract archive entry %q: %v", file.Name, err)
				}
			}
		})
	}
}

func TestCreateDownloadArchiveRejectsUnavailableFiles(t *testing.T) {
	root := t.TempDir()
	outside := writeTransferFixture(t, t.TempDir(), "outside.txt", []byte("outside"))
	for name, tempPath := range map[string]string{
		"missing": "",
		"outside": outside,
	} {
		t.Run(name, func(t *testing.T) {
			archivePath, err := CreateDownloadArchive(root, BatchRecord{Items: []Record{{
				Status: StatusCompleted, TempPath: tempPath, FileName: "file.txt", RemotePath: "/file.txt",
			}}})
			if err == nil || !strings.Contains(err.Error(), "no longer available") {
				t.Fatalf("archive error = %v", err)
			}
			if archivePath != "" {
				t.Fatalf("failed archive exposed path %q", archivePath)
			}
		})
	}
}

func TestTransferPathAndProgressHelpers(t *testing.T) {
	if got := UniqueArchiveEntryName("fallback.txt", "/outside/file.txt", "/reports", archivepath.NewTracker()); got != "fallback.txt" {
		t.Fatalf("fallback archive name = %q", got)
	}
	if got := RelativeArchiveEntryPath("/reports/2026/daily.csv", "/reports"); got != "2026/daily.csv" {
		t.Fatalf("relative archive path = %q", got)
	}
	if got := RelativeArchiveEntryPath("/other/daily.csv", "/reports"); got != "" {
		t.Fatalf("outside archive path = %q", got)
	}

	fileCases := map[string]string{
		" /var/log/../log/app.log ": "/var/log/app.log",
	}
	for input, want := range fileCases {
		if got, err := NormalizeRemoteFilePath(input); err != nil || got != want {
			t.Fatalf("NormalizeRemoteFilePath(%q) = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"", "relative.txt", "/", "/tmp/bad\nname"} {
		if _, err := NormalizeRemoteFilePath(input); err == nil {
			t.Fatalf("expected remote file %q to fail", input)
		}
	}
	if _, err := NormalizeRemoteFilePath("/" + strings.Repeat("x", 4097)); err == nil {
		t.Fatal("expected oversized remote file path to fail")
	}
	for _, input := range []string{"/" + strings.Repeat("界", 86), "/reports/" + strings.Repeat("🙂", 64) + "/daily.csv"} {
		if _, err := NormalizeRemoteFilePath(input); err == nil {
			t.Fatalf("expected non-portable remote component %q to fail", input)
		}
	}

	if got, err := NormalizeRemoteDirectoryPath(""); err != nil || got != "/" {
		t.Fatalf("empty remote directory = %q, %v", got, err)
	}
	if got, err := NormalizeRemoteDirectoryPath("/tmp/../var"); err != nil || got != "/var" {
		t.Fatalf("remote directory = %q, %v", got, err)
	}
	for _, input := range []string{"relative", "/tmp/bad\tname", "/" + strings.Repeat("x", 4097)} {
		if _, err := NormalizeRemoteDirectoryPath(input); err == nil {
			t.Fatalf("expected remote directory %q to fail", input)
		}
	}

	if got, err := NormalizeRelativeTransferPath(`reports\2026\daily.csv`); err != nil || got != "reports/2026/daily.csv" {
		t.Fatalf("relative transfer path = %q, %v", got, err)
	}
	for _, input := range []string{"", "/absolute", "../outside", "nested/../../outside", "bad\nname", strings.Repeat("x", 4097)} {
		if _, err := NormalizeRelativeTransferPath(input); err == nil {
			t.Fatalf("expected relative path %q to fail", input)
		}
	}
	if _, err := NormalizeRelativeTransferPath("reports/" + strings.Repeat("🙂", 64) + "/daily.csv"); err == nil {
		t.Fatal("expected oversized multibyte relative component to fail")
	}

	if got := JoinRemoteFilePath("/tmp", "../report.txt"); got != "/tmp/report.txt" {
		t.Fatalf("joined file path = %q", got)
	}
	if got := JoinRemoteFilePath("/", "..."); got != "/..." {
		t.Fatalf("root file path = %q", got)
	}
	if got := JoinRemoteRelativePath("/", "nested/file"); got != "/nested/file" {
		t.Fatalf("root relative path = %q", got)
	}
	if got := JoinRemoteRelativePath("/tmp/", "/nested/file"); got != "/tmp/nested/file" {
		t.Fatalf("joined relative path = %q", got)
	}

	if speed, eta := SpeedAndETA(0, 100, time.Second); speed != 0 || eta != -1 {
		t.Fatalf("empty progress = %d, %d", speed, eta)
	}
	if speed, eta := SpeedAndETA(50, 100, time.Second); speed != 50 || eta != 1 {
		t.Fatalf("active progress = %d, %d", speed, eta)
	}
	if speed, eta := SpeedAndETA(100, 100, time.Second); speed != 100 || eta != 0 {
		t.Fatalf("complete progress = %d, %d", speed, eta)
	}
}

func TestTempPathBoundaryAndScheduledCleanup(t *testing.T) {
	root := t.TempDir()
	inside := writeTransferFixture(t, root, "inside", []byte("data"))
	outside := writeTransferFixture(t, t.TempDir(), "outside", []byte("data"))
	if !TempPathAllowed(root, inside) || TempPathAllowed(root, root) || TempPathAllowed(root, outside) {
		t.Fatal("temporary path boundary accepted an invalid path")
	}

	ScheduleTempCleanup(root, outside, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside path was removed: %v", err)
	}
	ScheduleTempCleanup(root, inside, time.Millisecond)
	deadline := time.Now().Add(time.Second)
	for {
		_, err := os.Stat(inside)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scheduled cleanup did not remove file: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSafeFileNameBoundsAndSanitizesValues(t *testing.T) {
	if got := SafeFileName(" ../report.txt "); got != "report.txt_" {
		t.Fatalf("safe file name = %q", got)
	}
	if got := SafeFileName(".env"); got != ".env" {
		t.Fatalf("leading-dot safe file name = %q", got)
	}
	if got := SafeFileName(".report. "); got != ".report__" {
		t.Fatalf("edge-character safe file name = %q", got)
	}
	if got := SafeFileName("bad\nname.txt"); got != "badname.txt" {
		t.Fatalf("control-safe file name = %q", got)
	}
	if got := SafeFileName(strings.Repeat("x", 200)); len([]rune(got)) != 160 {
		t.Fatalf("bounded safe file name length = %d", len([]rune(got)))
	}
}

func TestValidateFileNamePreservesSupportedNamesAndRejectsUnsafeValues(t *testing.T) {
	for _, value := range []string{".env", ".report.", " report.txt ", "release:notes.txt"} {
		if err := ValidateFileName(value); err != nil {
			t.Errorf("supported file name %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"", "   ", ".", "..", "folder/report.txt", `folder\report.txt`, "bad\nname", strings.Repeat("x", 161), strings.Repeat("🙂", 64)} {
		if err := ValidateFileName(value); err == nil {
			t.Errorf("unsafe file name %q accepted", value)
		}
	}
}

func TestNormalizeCreateRequestDoesNotRewriteFileIdentity(t *testing.T) {
	request := CreateRequest{
		RuntimeID:  1,
		Direction:  DirectionUpload,
		Source:     SourceUI,
		LocalPath:  " local file ",
		RemotePath: "/remote/.env",
		FileName:   ".env",
		TempPath:   "/tmp/staged file ",
	}

	normalized, err := normalizeCreateRequest(request)
	if err != nil {
		t.Fatalf("normalize create request: %v", err)
	}
	if normalized.LocalPath != request.LocalPath || normalized.FileName != request.FileName || normalized.TempPath != request.TempPath {
		t.Fatalf("file identity changed: %#v", normalized)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func writeTransferFixture(t *testing.T, root, name string, contents []byte) string {
	t.Helper()
	value := filepath.Join(root, name)
	if err := os.WriteFile(value, contents, 0o600); err != nil {
		t.Fatalf("write transfer fixture: %v", err)
	}
	return value
}

func testZipBytes(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("payload.txt")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write([]byte("payload")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}
