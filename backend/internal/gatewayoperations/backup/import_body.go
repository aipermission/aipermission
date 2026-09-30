package backup

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

const importBodyIdleTimeout = 30 * time.Second

var errImportBodyIdle = errors.New("database import body stopped making progress")

type importProgressReader struct {
	body        io.ReadCloser
	controller  *http.ResponseController
	ctx         context.Context
	cancel      context.CancelCauseFunc
	timeout     time.Duration
	mu          sync.Mutex
	finished    bool
	interrupted bool
}

// Transport deadlines unblock socket reads; closing the body also covers
// readers whose ResponseWriter cannot expose a transport deadline.
func guardImportBody(w http.ResponseWriter, r *http.Request, timeout time.Duration) (*http.Request, func()) {
	ctx, cancel := context.WithCancelCause(r.Context())
	reader := &importProgressReader{
		body: r.Body, controller: http.NewResponseController(w), ctx: ctx, cancel: cancel, timeout: timeout,
	}
	stop := context.AfterFunc(ctx, func() { reader.interrupt(context.Cause(ctx)) })
	request := r.WithContext(ctx)
	request.Body = struct {
		io.Reader
		io.Closer
	}{reader, reader.body}
	return request, func() {
		reader.mu.Lock()
		reader.finished = true
		stop()
		cancel(nil)
		_ = reader.controller.SetReadDeadline(time.Time{})
		reader.mu.Unlock()
	}
}

func (reader *importProgressReader) Read(buffer []byte) (int, error) {
	reader.mu.Lock()
	if err := context.Cause(reader.ctx); err != nil {
		reader.mu.Unlock()
		return 0, err
	}
	_ = reader.controller.SetReadDeadline(time.Now().Add(reader.timeout))
	reader.mu.Unlock()
	timer := time.AfterFunc(reader.timeout, func() { reader.interrupt(errImportBodyIdle) })
	n, err := reader.body.Read(buffer)
	if !timer.Stop() {
		reader.interrupt(errImportBodyIdle)
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		reader.interrupt(errImportBodyIdle)
	}
	if cause := context.Cause(reader.ctx); cause != nil {
		return 0, cause
	}
	return n, err
}

func (reader *importProgressReader) interrupt(cause error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.finished || reader.interrupted {
		return
	}
	reader.interrupted = true
	reader.cancel(cause)
	_ = reader.controller.SetReadDeadline(time.Now())
	_ = reader.body.Close()
}
