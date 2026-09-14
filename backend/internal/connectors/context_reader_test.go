package connectors

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReaderWithContextRejectsReadsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	buffer := make([]byte, 4)
	read, err := ReaderWithContext(ctx, strings.NewReader("data")).Read(buffer)
	if read != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("read=%d err=%v, want canceled before input is consumed", read, err)
	}
}
