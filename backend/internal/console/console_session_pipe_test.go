package console

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console/pipedrain"
)

type bufferInspectionReader struct {
	secret  []byte
	reads   int
	cleared bool
}

type blockedConsoleReader struct{ release <-chan struct{} }

func (reader blockedConsoleReader) Read([]byte) (int, error) {
	<-reader.release
	return 0, io.EOF
}

func TestConsolePipeConsumerReleasesSessionOwnershipWhenReaderStalls(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	consumed := make(chan struct{})
	go func() {
		pipedrain.Consume(ctx, pipedrain.Read(ctx, blockedConsoleReader{release: release}), func(string) {})
		close(consumed)
	}()
	cancel()
	select {
	case <-consumed:
	case <-time.After(time.Second):
		t.Fatal("pipe consumer retained the managed session after cancellation")
	}
}

func (reader *bufferInspectionReader) Read(buffer []byte) (int, error) {
	reader.reads++
	if reader.reads == 1 {
		return copy(buffer, reader.secret), nil
	}
	reader.cleared = bytes.Equal(buffer[:len(reader.secret)], make([]byte, len(reader.secret)))
	return 0, io.EOF
}

func TestConsolePipeClearsReusablePlaintextBufferBeforeNextRead(t *testing.T) {
	reader := &bufferInspectionReader{secret: []byte("temporary-secret-value")}
	for range pipedrain.Read(t.Context(), reader) {
	}
	if !reader.cleared {
		t.Fatal("pipe retained plaintext in its reusable read buffer")
	}
}
