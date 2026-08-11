package api

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func TestFileTransferRoutesReturnNotFoundForUnknownRuntime(t *testing.T) {
	fixture := newAPITestFixture(t)
	browse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/browse", "", browseRemoteFilesRequest{
		RuntimeID: 999999,
		Path:      "/",
	})
	if browse.Code != http.StatusNotFound {
		t.Fatalf("browse status = %d body=%s", browse.Code, browse.Body.String())
	}
	download := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/download", "", startDownloadRequest{
		RuntimeID:  999999,
		RemotePath: "/missing.txt",
	})
	if download.Code != http.StatusNotFound {
		t.Fatalf("download status = %d body=%s", download.Code, download.Body.String())
	}
}

func TestUniqueArchiveEntryNameAvoidsDuplicateBasenames(t *testing.T) {
	used := map[string]int{}

	first := uniqueArchiveEntryName("app.log", "/var/log/app.log", "/", used)
	second := uniqueArchiveEntryName("app.log", "/tmp/app.log", "/", used)
	third := uniqueArchiveEntryName("", "/opt/app.log", "/", used)

	if first != "var/log/app.log" {
		t.Fatalf("unexpected first archive name: %s", first)
	}
	if second != "tmp/app.log" {
		t.Fatalf("remote directories should distinguish duplicate basenames, got %s", second)
	}
	if third != "opt/app.log" {
		t.Fatalf("remote path should determine the archive entry, got %s", third)
	}
}

func TestUniqueArchiveEntryNameTracksGeneratedSuffixes(t *testing.T) {
	used := map[string]int{}
	names := []string{
		uniqueArchiveEntryName("", "/a/f.txt", "/", used),
		uniqueArchiveEntryName("", `/a\f.txt`, "/", used),
		uniqueArchiveEntryName("", "/a/f-2.txt", "/", used),
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Fatalf("duplicate generated archive entry %q in %#v", name, names)
		}
		seen[name] = true
	}
}

func TestNormalizeRelativeTransferPathPreservesFoldersAndRejectsTraversal(t *testing.T) {
	value, err := normalizeRelativeTransferPath(`reports\2026\daily.csv`)
	if err != nil || value != "reports/2026/daily.csv" {
		t.Fatalf("normalize relative transfer path: value=%q err=%v", value, err)
	}
	for _, invalid := range []string{"../secret.txt", "reports/../../secret.txt", "/absolute.txt"} {
		if _, err := normalizeRelativeTransferPath(invalid); err == nil {
			t.Fatalf("expected %q to be rejected", invalid)
		}
	}
	if joined := joinRemoteRelativePath("/archive", value); joined != "/archive/reports/2026/daily.csv" {
		t.Fatalf("joined relative transfer path = %q", joined)
	}
}

func TestValidateStagedUploadSizeEnforcesObjectAndBatchLimits(t *testing.T) {
	if total, err := validateStagedUploadSize(maxFileTransferObjectBytes, maxFileTransferObjectBytes); err != nil || total != maxFileTransferBatchBytes {
		t.Fatalf("expected exact limits to pass: total=%d err=%v", total, err)
	}
	if _, err := validateStagedUploadSize(maxFileTransferObjectBytes+1, 0); err == nil {
		t.Fatal("expected oversized object to fail")
	}
	if _, err := validateStagedUploadSize(1, maxFileTransferBatchBytes); err == nil {
		t.Fatal("expected oversized batch to fail")
	}
}

func TestDownloadArchivePreservesNestedZipBytes(t *testing.T) {
	tempDir := t.TempDir()
	server := &Server{config: config.Config{DataPath: filepath.Join(tempDir, "data", "test.aipdb")}}
	handlers := fileTransferHandlers{server}
	root, err := handlers.ensureFileTransferTempRoot()
	if err != nil {
		t.Fatalf("create transfer temp root: %v", err)
	}
	innerZipPath := filepath.Join(root, "inner.zip")
	innerZipBytes := makeTestZip(t)
	if err := os.WriteFile(innerZipPath, innerZipBytes, 0o600); err != nil {
		t.Fatalf("write inner zip: %v", err)
	}
	textPath := filepath.Join(root, "readme.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write text: %v", err)
	}

	archivePath, err := handlers.createDownloadArchive(filetransfer.BatchRecord{
		Items: []filetransfer.Record{
			{Status: filetransfer.StatusCompleted, TempPath: innerZipPath, FileName: "inner.zip", RemotePath: "/tmp/inner.zip"},
			{Status: filetransfer.StatusCompleted, TempPath: textPath, FileName: "readme.txt", RemotePath: "/tmp/readme.txt"},
		},
	})
	if err != nil {
		t.Fatalf("create download archive: %v", err)
	}

	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open outer archive: %v", err)
	}
	defer archive.Close()
	var nested []byte
	for _, file := range archive.File {
		if file.Name != "inner.zip" {
			continue
		}
		if file.Method != zip.Store {
			t.Fatalf("nested zip entries should be stored without recompression, got method %d", file.Method)
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open nested zip entry: %v", err)
		}
		nested, err = io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatalf("read nested zip entry: %v", err)
		}
	}
	if !bytes.Equal(nested, innerZipBytes) {
		t.Fatalf("nested zip bytes changed: got %d bytes want %d", len(nested), len(innerZipBytes))
	}
	if _, err := zip.NewReader(bytes.NewReader(nested), int64(len(nested))); err != nil {
		t.Fatalf("nested zip should remain readable: %v", err)
	}
}

func TestDownloadArchivePreservesRelativeRemoteHierarchy(t *testing.T) {
	tempDir := t.TempDir()
	server := &Server{config: config.Config{DataPath: filepath.Join(tempDir, "data", "test.aipdb")}}
	handlers := fileTransferHandlers{server}
	root, err := handlers.ensureFileTransferTempRoot()
	if err != nil {
		t.Fatalf("create transfer temp root: %v", err)
	}
	firstPath := filepath.Join(root, "first")
	secondPath := filepath.Join(root, "second")
	if err := os.WriteFile(firstPath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}

	archivePath, err := handlers.createDownloadArchive(filetransfer.BatchRecord{Items: []filetransfer.Record{
		{Status: filetransfer.StatusCompleted, TempPath: firstPath, FileName: "a.txt", RemotePath: "/daily/a.txt"},
		{Status: filetransfer.StatusCompleted, TempPath: secondPath, FileName: "b.txt", RemotePath: "/daily/nested/b.txt"},
	}})
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer archive.Close()
	if len(archive.File) != 2 || archive.File[0].Name != "a.txt" || archive.File[1].Name != "nested/b.txt" {
		t.Fatalf("unexpected archive entries: %#v", archive.File)
	}
}

func makeTestZip(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create("payload.txt")
	if err != nil {
		t.Fatalf("create nested zip entry: %v", err)
	}
	if _, err := file.Write([]byte("zip payload")); err != nil {
		t.Fatalf("write nested zip entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close nested zip: %v", err)
	}
	return buffer.Bytes()
}
