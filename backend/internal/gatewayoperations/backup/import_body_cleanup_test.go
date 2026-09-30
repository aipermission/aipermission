package backup

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestImportBodyTimeoutRemovesPartialMultipartTempFiles(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	parts := multipart.NewWriter(writer)
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", reader)
	request.Header.Set("Content-Type", parts.FormDataContentType())
	guarded, finish := guardImportBody(httptest.NewRecorder(), request, 50*time.Millisecond)
	defer finish()
	written := make(chan error, 1)
	go func() {
		part, err := parts.CreateFormFile("sqlite", "incomplete.aipdb")
		if err == nil {
			_, err = io.Copy(part, strings.NewReader(strings.Repeat("x", 8192)))
		}
		written <- err
	}()
	err := guarded.ParseMultipartForm(1024)
	if guarded.MultipartForm != nil {
		defer guarded.MultipartForm.RemoveAll()
	}
	if !errors.Is(context.Cause(guarded.Context()), errImportBodyIdle) || err == nil {
		t.Fatalf("partial upload error=%v cause=%v", err, context.Cause(guarded.Context()))
	}
	if err := <-written; err != nil {
		t.Fatalf("upload prefix failed: %v", err)
	}
	files, err := os.ReadDir(temp)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("partial multipart upload retained %d temporary files", len(files))
	}
}

func TestCanceledImportDoesNotBeginAPasswordAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", strings.NewReader("incomplete")).WithContext(ctx)
	request.Header.Set("Content-Type", "multipart/form-data; boundary=example")
	called := false
	component := New(Dependencies{BeginAttempt: func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool) {
		called = true
		return nil, false
	}})
	response := httptest.NewRecorder()
	component.HTTPHandlers().Import(response, request)
	if response.Code != http.StatusRequestTimeout || called {
		t.Fatalf("canceled import status=%d password attempted=%t", response.Code, called)
	}
	response = httptest.NewRecorder()
	component.installImportedDatabase(response, request, "Example", "fixture-password", func(string) error { t.Fatal("writer ran"); return nil }, nil, nil)
	if response.Code != http.StatusRequestTimeout || called {
		t.Fatalf("canceled installation status=%d password attempted=%t", response.Code, called)
	}
}
