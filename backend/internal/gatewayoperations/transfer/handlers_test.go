package gatewaytransfer

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type reflectingTransferErrorPresenter struct {
	rejectingTransferAdapter
	responseValue string
	headerKey     string
	headerValue   string
}

func (presenter reflectingTransferErrorPresenter) PresentConnectorError(_ error) (connectorapi.ErrorPresentation, bool) {
	if presenter.responseValue == "" {
		return connectorapi.ErrorPresentation{}, false
	}
	header := http.Header{}
	if presenter.headerKey != "" {
		header.Add(presenter.headerKey, presenter.headerValue)
	}
	return connectorapi.ErrorPresentation{
		StatusCode: http.StatusConflict,
		Header:     header,
		Payload:    map[string]string{"detail": presenter.responseValue},
	}, true
}

func (presenter reflectingTransferErrorPresenter) ConnectorErrorMessage(prefix string, _ error) string {
	if presenter.responseValue == "" {
		return prefix
	}
	return prefix + ": " + presenter.responseValue
}

func TestFileTransferConnectorErrorsDoNotReflectCredentials(t *testing.T) {
	fixture := newTransferTestFixture(t)

	for _, testCase := range []struct {
		name       string
		presenter  reflectingTransferErrorPresenter
		err        error
		wantStatus int
		wantBody   string
		credential string
	}{
		{name: "raw connector error", err: errors.New("remote rejected secret"), wantStatus: http.StatusBadGateway, wantBody: "remote operation failed"},
		{name: "safe structured error", presenter: reflectingTransferErrorPresenter{responseValue: "approve host key"}, err: errors.New("remote rejected request"), wantStatus: http.StatusConflict, wantBody: "approve host key"},
		{name: "credential in payload", presenter: reflectingTransferErrorPresenter{responseValue: "secret"}, err: errors.New("remote rejected request"), wantStatus: http.StatusBadGateway, wantBody: "remote operation failed"},
		{name: "credential in header", presenter: reflectingTransferErrorPresenter{responseValue: "safe", headerKey: "X-Detail", headerValue: "secret"}, err: errors.New("remote rejected request"), wantStatus: http.StatusBadGateway, wantBody: "remote operation failed"},
		{name: "credential in header name", presenter: reflectingTransferErrorPresenter{responseValue: "safe", headerKey: "X-Secret", headerValue: "safe"}, err: errors.New("remote rejected request"), wantStatus: http.StatusBadGateway, wantBody: "remote operation failed", credential: "X-Secret"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			credential := testCase.credential
			if credential == "" {
				credential = "secret"
			}
			execution := transferExecution{adapter: testCase.presenter, boundary: actionresult.NewCredentialBoundary(map[string]any{"password": credential})}
			fixture.handlers.writeCredentialSafeConnectorError(response, execution, http.StatusBadGateway, "remote operation failed", testCase.err)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if bytes.Contains(response.Body.Bytes(), []byte("secret")) {
				t.Fatalf("credential leaked in response: %s", response.Body.String())
			}
			if !bytes.Contains(response.Body.Bytes(), []byte(testCase.wantBody)) {
				t.Fatalf("expected response missing: %s", response.Body.String())
			}
		})
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
	fixture := newTransferTestFixture(t)
	handlers := fixture.handlers
	root, err := handlers.runner.EnsureTempRoot(fixture.runtime)
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

	archivePath, err := handlers.runner.CreateDownloadArchive(fixture.runtime, filetransfer.BatchRecord{Items: []filetransfer.Record{
		{Status: filetransfer.StatusCompleted, TempPath: innerZipPath, FileName: "inner.zip", RemotePath: "/tmp/inner.zip"},
		{Status: filetransfer.StatusCompleted, TempPath: textPath, FileName: "readme.txt", RemotePath: "/tmp/readme.txt"},
	}})
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
	fixture := newTransferTestFixture(t)
	handlers := fixture.handlers
	root, err := handlers.runner.EnsureTempRoot(fixture.runtime)
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

	archivePath, err := handlers.runner.CreateDownloadArchive(fixture.runtime, filetransfer.BatchRecord{Items: []filetransfer.Record{
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
