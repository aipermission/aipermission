package execution

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type closeFailingWriter struct {
	bytes.Buffer
	closed bool
	err    error
}

func (writer *closeFailingWriter) Close() error {
	writer.closed = true
	return writer.err
}

func TestDownloadCloseFailureCannotReportSuccess(t *testing.T) {
	closeErr := errors.New("delayed write failed")
	local := &closeFailingWriter{err: closeErr}
	copied, checksum, err := copyAndCloseDownloadedFile(context.Background(), local, strings.NewReader("data"), 4, TransferOptions{})
	if !local.closed || !errors.Is(err, closeErr) || copied != 0 || checksum != "" {
		t.Fatalf("closed=%t copied=%d checksum=%q err=%v", local.closed, copied, checksum, err)
	}
}

func TestDownloadCopyFailureStillClosesLocalFile(t *testing.T) {
	local := &closeFailingWriter{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := copyAndCloseDownloadedFile(ctx, local, strings.NewReader("data"), 4, TransferOptions{})
	if !local.closed || !errors.Is(err, context.Canceled) {
		t.Fatalf("closed=%t err=%v", local.closed, err)
	}
}
