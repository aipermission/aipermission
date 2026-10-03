package backup

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Stop=false also means a callback is queued, not necessarily that it owns an
// unfinished read. The controlled timer models that exact scheduling boundary.
type queuedImportTimer struct{}

func (queuedImportTimer) Stop() bool { return false }

func importTimerFixture(t *testing.T, body io.ReadCloser) (*importProgressReader, <-chan func()) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(t.Context())
	t.Cleanup(func() { cancel(nil); _ = body.Close() })
	callbacks := make(chan func(), 4)
	reader := &importProgressReader{
		body: body, ctx: ctx, cancel: cancel, timeout: time.Hour,
		controller: http.NewResponseController(httptest.NewRecorder()),
		newTimer: func(_ time.Duration, callback func()) importIdleTimer {
			callbacks <- callback
			return queuedImportTimer{}
		},
	}
	return reader, callbacks
}

func TestImportQueuedTimerCannotCancelCompletedProgress(t *testing.T) {
	reader, callbacks := importTimerFixture(t, io.NopCloser(strings.NewReader("complete")))
	buffer := make([]byte, 8)
	n, err := reader.Read(buffer)
	if err != nil || string(buffer[:n]) != "complete" {
		t.Fatalf("completed progress = %q %v", buffer[:n], err)
	}
	(<-callbacks)()
	if cause := context.Cause(reader.ctx); cause != nil {
		t.Fatalf("queued completed timer canceled progress: %v", cause)
	}
}

func TestImportQueuedTimerCannotCancelNextReadButCurrentTimerStillCan(t *testing.T) {
	body, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	reader, callbacks := importTimerFixture(t, body)
	read := func() <-chan error {
		done := make(chan error, 1)
		go func() { _, err := reader.Read(make([]byte, 4)); done <- err }()
		return done
	}
	first := read()
	previousTimer := <-callbacks
	if _, err := writer.Write([]byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	second := read()
	currentTimer := <-callbacks
	previousTimer()
	if cause := context.Cause(reader.ctx); cause != nil {
		t.Fatalf("previous timer canceled next read: %v", cause)
	}
	currentTimer()
	select {
	case err := <-second:
		if !errors.Is(err, errImportBodyIdle) || !errors.Is(context.Cause(reader.ctx), errImportBodyIdle) {
			t.Fatalf("current timeout = %v / %v", err, context.Cause(reader.ctx))
		}
	case <-time.After(time.Second):
		t.Fatal("current timer did not interrupt blocked read")
	}
}
