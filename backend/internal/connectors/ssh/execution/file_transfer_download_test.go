package execution

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
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
	if !local.closed || !errors.Is(err, closeErr) || copied != 4 || checksum != "" {
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

type segmentedDownloadReader struct {
	parts [][]byte
}

func (reader *segmentedDownloadReader) Read(buffer []byte) (int, error) {
	if len(reader.parts) == 0 {
		return 0, errors.New("fixture read failed")
	}
	part := reader.parts[0]
	reader.parts = reader.parts[1:]
	return copy(buffer, part), nil
}

func TestDownloadByteLimitPreservesPartialEvidenceAndCloses(t *testing.T) {
	local := &closeFailingWriter{}
	reader := &segmentedDownloadReader{parts: [][]byte{[]byte("1234"), []byte("56789")}}
	bytes, checksum, err := copyAndCloseDownloadedFile(t.Context(), local, reader, 1, TransferOptions{MaxBytes: 8})
	if !errors.Is(err, connectors.ErrTransferByteLimit) || !local.closed || bytes != 4 || local.String() != "1234" || checksum != "" {
		t.Fatalf("partial download lost or exceeded its cap: bytes=%d body=%q checksum=%q closed=%v err=%v", bytes, local.String(), checksum, local.closed, err)
	}
}

func TestDownloadExactByteLimitAndPartialReadFailure(t *testing.T) {
	local := &closeFailingWriter{}
	bytes, checksum, err := copyAndCloseDownloadedFile(t.Context(), local, strings.NewReader("12345678"), 1, TransferOptions{MaxBytes: 8})
	if err != nil || bytes != 8 || checksum == "" || !local.closed {
		t.Fatalf("exact limit rejected: bytes=%d checksum=%q closed=%v err=%v", bytes, checksum, local.closed, err)
	}
	local = &closeFailingWriter{}
	bytes, checksum, err = copyAndCloseDownloadedFile(t.Context(), local, &segmentedDownloadReader{parts: [][]byte{[]byte("1234")}}, 1, TransferOptions{MaxBytes: 8})
	if err == nil || bytes != 4 || checksum != "" || !local.closed {
		t.Fatalf("partial read failure lost written bytes: bytes=%d checksum=%q closed=%v err=%v", bytes, checksum, local.closed, err)
	}
}
