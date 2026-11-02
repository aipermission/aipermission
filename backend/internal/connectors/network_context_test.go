package connectors

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestNetworkConnectionFollowsOperationCancellation(t *testing.T) {
	for _, alreadyCanceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		if alreadyCanceled {
			cancel()
		}
		client, peer := net.Pipe()
		bound := BindNetworkConnectionContext(ctx, client)
		t.Cleanup(func() { _ = bound.Close(); _ = peer.Close(); cancel() })
		cancel()
		_ = peer.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := peer.Read(make([]byte, 1)); err != io.EOF {
			t.Fatalf("cancellation did not close socket: %v", err)
		}
	}
}

func TestNetworkConnectionCloseReleasesCancellationHook(t *testing.T) {
	client, peer := net.Pipe()
	defer peer.Close()
	stopped := false
	bound := &contextNetworkConn{Conn: client, stop: func() bool { stopped = true; return true }}
	if err := bound.Close(); err != nil || !stopped {
		t.Fatalf("close did not release cancellation hook: %v", err)
	}
}
