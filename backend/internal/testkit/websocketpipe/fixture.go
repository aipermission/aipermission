// Package websocketpipe provides owned HTTP/WebSocket fixtures without a
// listening port. Framing and upgrade behavior belong to the standard server
// and Gorilla, not to a test protocol implementation.
package websocketpipe

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func Pair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	upgraded := make(chan *websocket.Conn, 1)
	client := Dial(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade fixture: %v", err)
			return
		}
		upgraded <- server
	}))
	select {
	case server := <-upgraded:
		t.Cleanup(func() { _ = server.Close() })
		return server, client
	case <-time.After(5 * time.Second):
		t.Fatal("fixture upgrade did not finish")
		return nil, nil
	}
}

// Dial runs the supplied real handler over a single full-duplex connection.
// Cleanup closes both endpoints before joining the handler and server.
func Dial(t *testing.T, handler http.Handler) *websocket.Conn {
	t.Helper()
	clientPipe, serverPipe := net.Pipe()
	_ = clientPipe.SetDeadline(time.Now().Add(10 * time.Second))
	_ = serverPipe.SetDeadline(time.Now().Add(10 * time.Second))
	listener := &singleListener{pending: make(chan net.Conn, 1), closed: make(chan struct{}), conn: serverPipe}
	listener.pending <- serverPipe
	handlerDone, handlerStarted, serveDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		defer close(handlerDone)
		handler.ServeHTTP(w, r)
	})}
	go func() {
		defer close(serveDone)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			t.Errorf("serve fixture: %v", err)
		}
	}()
	t.Cleanup(func() {
		_ = clientPipe.Close()
		_ = serverPipe.Close()
		_ = server.Close()
		_ = listener.Close()
		join(t, serveDone)
		select {
		case <-handlerStarted:
			join(t, handlerDone)
		default:
		}
	})
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second, NetDialContext: func(context.Context, string, string) (net.Conn, error) { return clientPipe, nil }}
	client, _, err := dialer.DialContext(t.Context(), "ws://console.fixture/", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func join(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("websocket fixture work did not finish")
	}
}

type singleListener struct {
	pending chan net.Conn
	closed  chan struct{}
	conn    net.Conn
	once    sync.Once
}

func (l *singleListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.pending:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *singleListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return l.conn.Close()
}

func (l *singleListener) Addr() net.Addr { return l.conn.LocalAddr() }
