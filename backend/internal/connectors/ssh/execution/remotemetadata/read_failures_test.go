package remotemetadata

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

type failedMetadataSession struct {
	*fakeRemoteMetadataSession
	failure error
}

func (session failedMetadataSession) Run(command string) error {
	if err := session.fakeRemoteMetadataSession.Run(command); err != nil {
		return err
	}
	return session.failure
}

func TestReadRejectsMetadataAfterCommandFailure(t *testing.T) {
	failure := errors.New("stat command failed after producing output")
	base := newFakeRemoteMetadataSession([][]byte{[]byte("gnu 81c0 0 0")}, false)
	got, err := Read(t.Context(), failedMetadataSession{base, failure}, "/safe/file")
	if !errors.Is(err, failure) || got != (Metadata{}) {
		t.Fatalf("failed command returned usable metadata: %#v, %v", got, err)
	}
	select {
	case <-base.closed:
	default:
		t.Fatal("failed metadata session leaked")
	}
}

func TestParseRetainsCompleteModeBits(t *testing.T) {
	got, err := parse("gnu 8fff 0 0")
	want := os.FileMode(0o777) | os.ModeSetuid | os.ModeSetgid | os.ModeSticky
	if err != nil || !got.Mode.IsRegular() || got.Mode != want {
		t.Fatalf("special permission metadata = %#v, %v; want %#o", got, err, want)
	}
	got, err = parse("gnu a1ff 0 0")
	if err != nil || got.Mode.IsRegular() || got.Mode&os.ModeSymlink == 0 {
		t.Fatalf("symlink was normalized to regular file: %#v, %v", got, err)
	}
}

func TestBoundedOutputClosesOnceAndNeverRetainsExcessBytes(t *testing.T) {
	var closeCalls atomic.Int64
	output := newBoundedOutput(4, func() error { closeCalls.Add(1); return nil })
	var workers sync.WaitGroup
	for range 12 {
		workers.Go(func() {
			if n, err := output.Write([]byte("oversized")); n != 9 || err != nil {
				t.Errorf("draining write = %d, %v", n, err)
			}
			_ = output.String()
			_ = output.Exceeded()
		})
	}
	workers.Wait()
	if !output.Exceeded() || len(output.String()) != 4 || closeCalls.Load() != 1 {
		t.Fatalf("bounded output exceeded contract: bytes=%q closed=%d", output.String(), closeCalls.Load())
	}
}

func TestReadReturnsCancellationForAlreadyCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	base := newFakeRemoteMetadataSession([][]byte{[]byte("gnu 81c0 0 0")}, false)
	got, err := Read(ctx, base, "/safe/file")
	if !errors.Is(err, context.Canceled) || got != (Metadata{}) {
		t.Fatalf("canceled request returned usable metadata: %#v, %v", got, err)
	}
}
