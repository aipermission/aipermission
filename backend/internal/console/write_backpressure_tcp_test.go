package console

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Real OS-buffer coverage complements the deterministic in-memory transport.
// This test is mandatory; unavailable loopback sockets are not a skip/pass.
func TestConsoleLiveBackpressureTCP(t *testing.T) {
	// Saturate the socket with a larger payload without timing thousands of
	// display-filter regex calls as part of the network-write deadline.
	const recordBytes = 4096
	const prefix = "fixture-output "
	record := prefix + strings.Repeat("x", recordBytes-len(prefix)-2) + "\r\n"
	testConsoleLiveBackpressure(t,
		func(t *testing.T) (*websocket.Conn, *websocket.Conn) { return newConsoleTCPFramePair(t, true) },
		func(t *testing.T) (*websocket.Conn, *websocket.Conn) { return newConsoleTCPFramePair(t, false) },
		strings.Repeat(record, 128))
}

func newConsoleTCPFramePair(t *testing.T, backpressured bool) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	upgraded := make(chan *websocket.Conn, 1)
	handlerStarted, handlerDone := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		defer close(handlerDone)
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade TCP fixture: %v", err)
			return
		}
		t.Cleanup(func() { _ = ws.Close() })
		if backpressured {
			if err := ws.UnderlyingConn().(*net.TCPConn).SetWriteBuffer(1024); err != nil {
				_ = ws.Close()
				t.Errorf("bound TCP send buffer: %v", err)
				return
			}
		}
		upgraded <- ws
	}))
	t.Cleanup(func() {
		server.Close()
		select {
		case <-handlerStarted:
			waitConsoleFrameWork(t, handlerDone)
		default:
		}
	})
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if backpressured {
		// Only the nonreader has a tiny receive window. Constraining the healthy
		// reader too turns the eviction test into an OS window-recovery test.
		if err := client.UnderlyingConn().(*net.TCPConn).SetReadBuffer(1024); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case peer := <-upgraded:
		return peer, client
	case <-time.After(5 * time.Second):
		t.Fatal("TCP fixture upgrade did not finish")
		return nil, nil
	}
}
