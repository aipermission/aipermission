package socketwrite_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/socketwrite"
	"github.com/aipermission/aipermission/backend/internal/testkit/websocketpipe"
	"github.com/gorilla/websocket"
)

func TestUnavailableWriters(t *testing.T) {
	for _, mu := range []*sync.Mutex{nil, {}} {
		if err := socketwrite.JSON(nil, mu, "fixture"); !errors.Is(err, socketwrite.ErrUnavailable) {
			t.Fatalf("JSON unavailable error: %v", err)
		}
		if err := socketwrite.Control(nil, mu, websocket.PingMessage, nil); !errors.Is(err, socketwrite.ErrUnavailable) {
			t.Fatalf("control unavailable error: %v", err)
		}
	}
	socketwrite.KeepAlive(nil, nil, time.Second, nil)
}

func TestJSONPreservesMessageAndReleasesMutex(t *testing.T) {
	for _, mu := range []*sync.Mutex{nil, {}} {
		server, client := websocketpipe.Pair(t)
		done := make(chan error, 1)
		writerDone := make(chan struct{})
		t.Cleanup(func() {
			_ = server.Close()
			_ = client.Close()
			joinSocketWriter(t, writerDone)
		})
		go func() {
			defer close(writerDone)
			done <- socketwrite.JSON(server, mu, map[string]string{"data": "fixture value"})
		}()
		_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
		var message map[string]string
		if err := client.ReadJSON(&message); err != nil || message["data"] != "fixture value" {
			t.Fatalf("wire JSON changed: %#v %v", message, err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("JSON writer did not finish")
		}
		if mu != nil {
			if !mu.TryLock() {
				t.Fatal("successful writer retained mutex")
			}
			mu.Unlock()
		}
	}
}

func TestKeepAliveStopPreservesConnection(t *testing.T) {
	server, client := websocketpipe.Pair(t)
	stop, aliveDone, readerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ping := make(chan struct{}, 1)
	client.SetPingHandler(func(string) error {
		select {
		case ping <- struct{}{}:
		default:
		}
		return nil
	})
	readResult := make(chan error, 1)
	go func() {
		defer close(readerDone)
		_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
		var message map[string]string
		err := client.ReadJSON(&message)
		if err == nil && message["data"] != "still open" {
			err = errors.New("stopped keepalive changed subsequent JSON")
		}
		readResult <- err
	}()
	var stopOnce sync.Once
	stopAlive := func() { stopOnce.Do(func() { close(stop) }) }
	t.Cleanup(func() {
		stopAlive()
		_ = server.Close()
		_ = client.Close()
		joinSocketWriter(t, aliveDone)
		joinSocketWriter(t, readerDone)
	})
	go func() { defer close(aliveDone); socketwrite.KeepAlive(server, &sync.Mutex{}, time.Millisecond, stop) }()
	select {
	case <-ping:
	case <-time.After(5 * time.Second):
		t.Fatal("keepalive did not send a ping")
	}
	stopAlive()
	joinSocketWriter(t, aliveDone)
	if err := socketwrite.JSONLocked(server, map[string]string{"data": "still open"}); err != nil {
		t.Fatalf("normal stop closed connection: %v", err)
	}
	joinSocketWriter(t, readerDone)
	select {
	case err := <-readResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("JSON readback did not finish")
	}
}

func TestKeepAliveBackpressureClosesSocketAndWakesReader(t *testing.T) {
	server, client := websocketpipe.Pair(t)
	readerDone, aliveDone := make(chan struct{}), make(chan struct{})
	readError := make(chan error, 1)
	go func() { defer close(readerDone); _, _, err := server.ReadMessage(); readError <- err }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
		joinSocketWriter(t, aliveDone)
		joinSocketWriter(t, readerDone)
	})
	go func() { defer close(aliveDone); socketwrite.KeepAlive(server, &sync.Mutex{}, time.Millisecond, nil) }()
	select {
	case <-aliveDone:
	case <-time.After(socketwrite.Timeout + 2*time.Second):
		t.Fatal("keepalive control write did not time out")
	}
	select {
	case <-readerDone:
		select {
		case err := <-readError:
			if err == nil {
				t.Fatal("failed keepalive left reader usable")
			}
		default:
			t.Fatal("reader exit was not reported")
		}
	case <-time.After(time.Second):
		t.Fatal("failed keepalive did not close the socket")
	}
}

func joinSocketWriter(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("socket writer fixture work did not finish")
	}
}
