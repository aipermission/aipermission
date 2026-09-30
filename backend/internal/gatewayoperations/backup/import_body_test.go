package backup

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestImportBodyClosesAnInactiveReaderWithoutTransportSupport(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", reader)
	guarded, finish := guardImportBody(httptest.NewRecorder(), request, 25*time.Millisecond)
	defer finish()
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(guarded.Body); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, errImportBodyIdle) || !errors.Is(context.Cause(guarded.Context()), errImportBodyIdle) {
			t.Fatalf("inactive body error=%v cause=%v", err, context.Cause(guarded.Context()))
		}
	case <-time.After(time.Second):
		_ = reader.Close()
		t.Fatal("inactive body read was not interrupted")
	}
	if request.Context().Err() != nil {
		t.Fatal("body guard changed the original request context")
	}
}

func TestImportBodyCancellationInterruptsABlockedRead(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", reader).WithContext(ctx)
	guarded, finish := guardImportBody(httptest.NewRecorder(), request, time.Hour)
	defer finish()
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(guarded.Body); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled body error=%v", err)
		}
	case <-time.After(time.Second):
		_ = reader.Close()
		t.Fatal("cancellation did not interrupt the blocked body")
	}
}

func TestImportBodyProgressRenewsItsInactivityWindow(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", reader)
	deadlines := &importDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	guarded, finish := guardImportBody(deadlines, request, 150*time.Millisecond)
	defer finish()
	done := make(chan error, 1)
	go func() {
		defer writer.Close()
		for index := 0; index < 12; index++ {
			if _, err := writer.Write([]byte("x")); err != nil {
				done <- err
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		done <- nil
	}()
	data, err := io.ReadAll(guarded.Body)
	if err != nil || string(data) != strings.Repeat("x", 12) {
		t.Fatalf("progressing upload data=%q error=%v", data, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	finish()
	deadlines.mu.Lock()
	defer deadlines.mu.Unlock()
	if len(deadlines.values) < 13 || !deadlines.values[len(deadlines.values)-1].IsZero() {
		t.Fatalf("read deadlines were not renewed and cleared: %v", deadlines.values)
	}
}

func TestImportBodyCompletedReadDoesNotLeaveAWatchdog(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", strings.NewReader("complete"))
	deadlines := &importDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	guarded, finish := guardImportBody(deadlines, request, 10*time.Millisecond)
	data, err := io.ReadAll(guarded.Body)
	if err != nil || string(data) != "complete" {
		t.Fatalf("read data=%q error=%v", data, err)
	}
	finish()
	time.Sleep(25 * time.Millisecond)
	deadlines.mu.Lock()
	defer deadlines.mu.Unlock()
	if !deadlines.values[len(deadlines.values)-1].IsZero() {
		t.Fatal("late watchdog restored a deadline after cleanup")
	}
}

type importDeadlineWriter struct {
	*httptest.ResponseRecorder
	mu     sync.Mutex
	values []time.Time
}

func (writer *importDeadlineWriter) SetReadDeadline(deadline time.Time) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.values = append(writer.values, deadline)
	return nil
}
