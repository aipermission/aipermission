package serviceboundary

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestFailedStreamCannotResumeOrFlushHeldData(t *testing.T) {
	var destination bytes.Buffer
	writer, err := testBoundary(t).NewScanningWriter(&destination)
	if err != nil {
		t.Fatal(err)
	}
	split := len(fixtureToken) / 2
	if _, err := writer.Write([]byte(fixtureToken[:split])); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(fixtureToken[split:])); !errors.Is(err, ErrReflectedCredential) {
		t.Fatalf("reflected stream = %v", err)
	}
	before := destination.Len()
	if written, err := writer.Write([]byte("clean")); written != 0 || !errors.Is(err, ErrReflectedCredential) {
		t.Errorf("resumed poisoned stream = %d, %v", written, err)
	}
	if err := writer.Flush(); !errors.Is(err, ErrReflectedCredential) || destination.Len() != before {
		t.Errorf("poisoned flush delivered held data: %v", err)
	}
}

type rejectedDestination struct {
	err      error
	short    bool
	partial  bool
	calls    int
	accepted bytes.Buffer
}

func (destination *rejectedDestination) Write(payload []byte) (int, error) {
	destination.calls++
	written := 0
	if destination.short {
		written = len(payload) - 1
	} else if destination.partial {
		written = len(payload) / 2
	}
	_, _ = destination.accepted.Write(payload[:written])
	if destination.short {
		return written, nil
	}
	return written, destination.err
}

func TestDestinationFailuresArePreservedAndPreventRetry(t *testing.T) {
	injected := errors.New("injected destination failure")
	for _, mode := range []string{"error", "short", "partial-error"} {
		for _, onFlush := range []bool{false, true} {
			destination := &rejectedDestination{err: injected, short: mode == "short", partial: mode == "partial-error"}
			cause := injected
			if destination.short {
				cause = io.ErrShortWrite
			}
			writer, err := testBoundary(t).NewScanningWriter(destination)
			if err != nil {
				t.Fatal(err)
			}
			payload := "safe"
			if !onFlush {
				payload = strings.Repeat("safe", 100)
			}
			written, first := writer.Write([]byte(payload))
			if onFlush {
				if first != nil || written != len(payload) {
					t.Fatalf("held stream = %d, %v", written, first)
				}
				first = writer.Flush()
			}
			if !errors.Is(first, cause) {
				t.Fatalf("destination cause lost: %v", first)
			}
			if destination.calls != 1 || (mode != "error" && destination.accepted.Len() == 0) {
				t.Fatalf("first failure did not exercise the destination: calls=%d bytes=%d", destination.calls, destination.accepted.Len())
			}
			accepted := bytes.Clone(destination.accepted.Bytes())
			if written, err := writer.Write([]byte("next")); written != 0 || !errors.Is(err, cause) {
				t.Errorf("failed destination retried = %d, %v", written, err)
			}
			if err := writer.Flush(); !errors.Is(err, cause) {
				t.Errorf("failed destination flush retried: %v", err)
			}
			if destination.calls != 1 || !bytes.Equal(destination.accepted.Bytes(), accepted) {
				t.Errorf("failed destination was written again: calls=%d accepted=%d", destination.calls, destination.accepted.Len())
			}
		}
	}
}

func TestUninitializedStreamFailsClosed(t *testing.T) {
	for _, writer := range []*ScanningWriter{nil, {}} {
		if written, err := writer.Write([]byte("clean")); written != 0 || !errors.Is(err, ErrUnavailable) {
			t.Errorf("uninitialized write = %d, %v", written, err)
		}
		if err := writer.Flush(); !errors.Is(err, ErrUnavailable) {
			t.Errorf("uninitialized flush = %v", err)
		}
	}
}
