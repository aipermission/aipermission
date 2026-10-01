package websocketpipe

import (
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestPairPreservesBidirectionalJSON(t *testing.T) {
	server, client := Pair(t)
	for _, direction := range []struct{ writer, reader *websocket.Conn }{{server, client}, {client, server}} {
		writerDone := make(chan struct{})
		writeError := make(chan error, 1)
		t.Cleanup(func() {
			_ = direction.writer.Close()
			_ = direction.reader.Close()
			join(t, writerDone)
		})
		go func() {
			defer close(writerDone)
			_ = direction.writer.SetWriteDeadline(time.Now().Add(2 * time.Second))
			writeError <- direction.writer.WriteJSON(map[string]string{"data": "owned fixture"})
		}()
		_ = direction.reader.SetReadDeadline(time.Now().Add(3 * time.Second))
		var message map[string]string
		if err := direction.reader.ReadJSON(&message); err != nil || message["data"] != "owned fixture" {
			t.Fatalf("bidirectional fixture JSON: %#v %v", message, err)
		}
		join(t, writerDone)
		select {
		case err := <-writeError:
			if err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatal("writer did not report completion")
		}
	}
}

func TestDialRunsAndJoinsOwnedHandler(t *testing.T) {
	handlerDone := make(chan struct{})
	client := Dial(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		server, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("owned handler upgrade: %v", err)
			return
		}
		defer server.Close()
		_ = server.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if err := server.WriteJSON(map[string]string{"data": "snapshot"}); err != nil {
			t.Errorf("owned handler snapshot: %v", err)
			return
		}
		_, _, _ = server.ReadMessage()
	}))
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	var message map[string]string
	if err := client.ReadJSON(&message); err != nil || message["data"] != "snapshot" {
		t.Fatalf("owned handler response: %#v %v", message, err)
	}
	_ = client.Close()
	join(t, handlerDone)
}

func TestSingleListenerCloseEndsAccept(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	listener := &singleListener{pending: make(chan net.Conn, 1), closed: make(chan struct{}), conn: server}
	if listener.Addr() != server.LocalAddr() {
		t.Fatal("listener address did not use owned connection")
	}
	for range 2 {
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := listener.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed listener kept accepting: %v", err)
	}
}
