package console

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/testkit/websocketpipe"
	"github.com/gorilla/websocket"
)

func TestConsoleOutputBelongsToSnapshotOrLiveDelivery(t *testing.T) {
	for _, path := range []string{"manual", "display"} {
		for _, registration := range []string{"before_append", "after_append"} {
			t.Run(path+"/"+registration, func(t *testing.T) {
				testConsoleOutputOwnership(t, path, registration)
			})
		}
	}
}

func testConsoleOutputOwnership(t *testing.T, path, registration string) {
	server, client := newConsoleFramePair(t)
	session := &managedConsoleSession{
		id: 7, status: "connected", manager: &Manager{}, workClosed: true,
		clients:       map[*websocket.Conn]*sync.Mutex{},
		pendingOutput: strings.Repeat("p", maxConsolePendingFlushSize),
	}
	data := "\rprogress\x1b[2K\rcomplete\r\n"
	if path == "display" {
		data = "[AI command] echo complete\r\ncomplete\r\n"
	}
	var writeMu *sync.Mutex
	var status, snapshot string
	var snapshotReleaseOnce sync.Once
	releaseSnapshot := func() {
		if writeMu != nil {
			snapshotReleaseOnce.Do(writeMu.Unlock)
		}
	}
	if registration == "before_append" {
		writeMu, status, snapshot = registerConsoleFrameClient(t, session, server)
	}
	// Pause the actual producer after transcript update but before emission using
	// the existing owned-work admission fence, not a production test hook.
	session.workMu.Lock()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(session.workMu.Unlock) }
	done := make(chan struct{})
	var writeDone chan struct{}
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
		release()
		if writeDone != nil {
			waitConsoleFrameWork(t, writeDone)
		} else {
			releaseSnapshot()
		}
		waitConsoleFrameWork(t, done)
	})
	go func() {
		defer close(done)
		if path == "manual" {
			session.appendSafeOutput(data)
		} else {
			session.appendDisplayOutput(data)
		}
		session.broadcast(ptyServerMessage{Type: "ready", Status: "connected", SessionID: session.id})
	}()
	waitConsoleTranscript(t, session, data)
	if registration == "after_append" {
		writeMu, status, snapshot = registerConsoleFrameClient(t, session, server)
	}
	// Release the producer while the per-client snapshot fence is still held.
	release()
	writeDone = make(chan struct{})
	go func() {
		defer close(writeDone)
		defer releaseSnapshot()
		_ = server.WriteJSON(ptyServerMessage{Type: "snapshot", Status: status, Data: snapshot, SessionID: session.id})
	}()
	first := readConsoleFrame(t, client)
	if first.Type != "snapshot" || first.Data != snapshot || first.SessionID != session.id {
		t.Fatalf("initial snapshot changed: %#v", first)
	}
	if registration == "before_append" {
		if snapshot != "" {
			t.Fatalf("pre-append snapshot contains later output: %q", snapshot)
		}
		live := readConsoleFrame(t, client)
		if live.Type != "output" || live.Data != data || live.SessionID != session.id {
			t.Fatalf("expected exact live output after snapshot: %#v", live)
		}
	} else if snapshot != data {
		t.Fatalf("post-append snapshot omitted output: %q", snapshot)
	}
	// A sentinel emitted after append returns proves no duplicate frame precedes
	// it; this assertion does not infer absence from a short read timeout.
	if next := readConsoleFrame(t, client); next.Type != "ready" {
		t.Fatalf("output was delivered both in snapshot and live: %#v", next)
	}
	waitConsoleFrameWork(t, writeDone)
	waitConsoleFrameWork(t, done)
}

func registerConsoleFrameClient(t *testing.T, session *managedConsoleSession, server *websocket.Conn) (*sync.Mutex, string, string) {
	t.Helper()
	writeMu, status, snapshot, err := session.addClientWithSnapshot(server)
	if err != nil {
		t.Fatal(err)
	}
	return writeMu, status, snapshot
}

func waitConsoleTranscript(t *testing.T, session *managedConsoleSession, expected string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		_, transcript := session.snapshot()
		if transcript == expected {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("producer did not reach the transcript boundary: %q", transcript)
		case <-ticker.C:
		}
	}
}

func readConsoleFrame(t *testing.T, client *websocket.Conn) ptyServerMessage {
	t.Helper()
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var message ptyServerMessage
	if err := client.ReadJSON(&message); err != nil {
		t.Fatalf("read websocket frame: %v", err)
	}
	return message
}

func waitConsoleFrameWork(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("websocket fixture work did not finish")
	}
}

// Use the real HTTP upgrade and Gorilla framing over an owned in-memory
// transport, so this test neither needs a listening port nor emulates frames.
func newConsoleFramePair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	return websocketpipe.Pair(t)
}
