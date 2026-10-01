package console

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/testkit/websocketpipe"
	"github.com/gorilla/websocket"
)

func TestConsoleJSONWriteEvictsBackpressureWithinDeadline(t *testing.T) {
	server, _ := websocketpipe.Pair(t)
	writeMu := &sync.Mutex{}
	finished := make(chan error, 1)
	go func() {
		finished <- writePTYMessage(server, writeMu, ptyServerMessage{Type: "output", Data: "unread output"})
	}()
	select {
	case err := <-finished:
		assertConsoleWriteTimeout(t, err)
	case <-time.After(4 * time.Second):
		_ = server.Close()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("blocked fixture writer did not stop")
		}
		t.Fatal("JSON write had no bounded deadline")
	}
	if !writeMu.TryLock() {
		t.Fatal("timed-out writer retained its client mutex")
	}
	writeMu.Unlock()
}

func TestConsoleInitialSnapshotBackpressureReleasesAttachAdmission(t *testing.T) {
	session, _, finished, handlerDone := newConsoleBackpressureAttach(t, "unread snapshot")
	select {
	case err := <-finished:
		assertConsoleWriteTimeout(t, err)
	case <-time.After(4 * time.Second):
		t.Fatal("snapshot write retained attach admission without a deadline")
	}
	waitConsoleFrameWork(t, handlerDone)
	assertConsoleBackpressureClientRemoved(t, session)
	drained := make(chan struct{})
	go func() { defer close(drained); session.drainOwnedWork() }()
	waitConsoleFrameWork(t, drained)
}

func newConsoleBackpressureAttach(t *testing.T, transcript string) (*managedConsoleSession, *websocket.Conn, <-chan error, <-chan struct{}) {
	t.Helper()
	session := &managedConsoleSession{
		id: 7, runtimeID: 8, generation: 9, principal: testExecutionPrincipal(), status: "connected",
		transcript: transcript, clients: map[*websocket.Conn]*sync.Mutex{},
	}
	manager := &Manager{sessions: map[int64]*managedConsoleSession{session.id: session}}
	session.manager = manager
	finished := make(chan error, 1)
	handlerDone := make(chan struct{})
	// The fixture's endpoint cleanup runs before this join on fatal paths.
	t.Cleanup(func() { waitConsoleFrameWork(t, handlerDone) })
	client := websocketpipe.Dial(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		finished <- manager.Attach(w, r, session.principal, session.id, func(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
			return (&websocket.Upgrader{}).Upgrade(w, r, nil)
		})
	}))
	return session, client, finished, handlerDone
}

func assertConsoleBackpressureClientRemoved(t *testing.T, session *managedConsoleSession) {
	t.Helper()
	session.mu.Lock()
	clients := len(session.clients)
	session.mu.Unlock()
	if clients != 0 {
		t.Fatalf("timed-out client stayed registered: %d", clients)
	}
}

func TestConsoleErrorResponseBackpressureClosesAttach(t *testing.T) {
	for _, input := range []string{"fixture input", strings.Repeat("x", maxConsoleInputBytes+1)} {
		name := "transport_error"
		if len(input) > maxConsoleInputBytes {
			name = "oversize_input"
		}
		t.Run(name, func(t *testing.T) {
			session, client, finished, handlerDone := newConsoleBackpressureAttach(t, "initial snapshot")
			if message := readConsoleFrame(t, client); message.Type != "snapshot" {
				t.Fatalf("unexpected initial message: %#v", message)
			}
			if err := client.WriteJSON(ptyClientMessage{Type: "input", Data: input}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				assertConsoleWriteTimeout(t, err)
			case <-time.After(4 * time.Second):
				t.Fatal("failed error-response write did not close Attach")
			}
			waitConsoleFrameWork(t, handlerDone)
			assertConsoleBackpressureClientRemoved(t, session)
		})
	}
}

func TestConsoleLiveBackpressurePreservesOtherClientsAndCommandMarkers(t *testing.T) {
	testConsoleLiveBackpressure(t, websocketpipe.Pair, websocketpipe.Pair, "fixture-output\r\n")
}

type consoleBackpressurePair func(*testing.T) (*websocket.Conn, *websocket.Conn)

func testConsoleLiveBackpressure(t *testing.T, slowPair, goodPair consoleBackpressurePair, data string) {
	slowServer, slowClient := slowPair(t)
	goodServer, goodClient := goodPair(t)
	ctx, cancel := context.WithCancel(t.Context())
	session := &managedConsoleSession{
		id: 7, status: "connected", manager: &Manager{}, ctx: ctx, workClosed: true,
		clients:    map[*websocket.Conn]*sync.Mutex{slowServer: {}, goodServer: {}},
		activeExec: &consoleSessionActiveExec{Command: "echo fixture-output", Marker: "__AIPERMISSION_EXIT_7"},
	}
	pongReadDone, pongWriteDone, readDone, consumeDone := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var pongs atomic.Int64
	stopPongs := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(stopPongs) }) }
	messages := make(chan ptyServerMessage, 8)
	readErrors := make(chan error, 1)
	t.Cleanup(func() {
		cancel()
		stop()
		for _, ws := range []*websocket.Conn{slowServer, slowClient, goodServer, goodClient} {
			_ = ws.Close()
		}
		for _, done := range []chan struct{}{pongReadDone, pongWriteDone, readDone, consumeDone} {
			waitConsoleFrameWork(t, done)
		}
	})
	_ = slowServer.SetReadDeadline(time.Now().Add(time.Second))
	slowServer.SetPongHandler(func(string) error {
		pongs.Add(1)
		return slowServer.SetReadDeadline(time.Now().Add(time.Second))
	})
	go func() {
		defer close(pongReadDone)
		_, _, _ = slowServer.ReadMessage()
	}()
	go sendConsoleBackpressurePongs(slowClient, stopPongs, pongWriteDone)
	go readConsoleBackpressureFrames(goodClient, messages, readErrors, readDone)
	output := make(chan RuntimeOutput, 2)
	completion := make(chan error, 1)
	output <- RuntimeOutput{Data: data}
	output <- RuntimeOutput{Data: "\r\n__AIPERMISSION_EXIT_7:0\r\n"}
	close(output)
	completion <- nil
	close(completion)
	go func() {
		defer close(consumeDone)
		session.consumeRuntime(&RuntimeSession{Output: output, Done: completion, Close: func() error { return nil }})
	}()
	select {
	case <-consumeDone:
	case <-time.After(4 * time.Second):
		t.Fatal("nonreading client blocked runtime output/command completion")
	}
	select {
	case <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("healthy client did not finish reading terminal frames")
	}
	select {
	case err := <-readErrors:
		t.Fatalf("healthy client did not receive terminal frames: %v", err)
	default:
	}
	var displayed strings.Builder
	for message := range messages {
		if message.Type == "output" {
			displayed.WriteString(message.Data)
		}
	}
	if !strings.Contains(displayed.String(), "fixture-output") || pongs.Load() == 0 {
		t.Fatalf("fixture did not sustain healthy output and slow-client pongs: output_bytes=%d pongs=%d", displayed.Len(), pongs.Load())
	}
	session.mu.Lock()
	_, slowPresent := session.clients[slowServer]
	_, goodPresent := session.clients[goodServer]
	session.mu.Unlock()
	if slowPresent || !goodPresent {
		t.Fatalf("wrong eviction: slow=%v healthy=%v", slowPresent, goodPresent)
	}
	outputText, exit, completed, err := session.checkCommandResult(0, "__AIPERMISSION_EXIT_7")
	if err != nil || !completed || exit != 0 || !strings.Contains(outputText, "fixture-output") {
		t.Fatalf("runtime marker was not consumed: completed=%v exit=%d output_bytes=%d err=%v", completed, exit, len(outputText), err)
	}
}

func sendConsoleBackpressurePongs(client *websocket.Conn, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := client.WriteControl(websocket.PongMessage, []byte("fixture"), time.Now().Add(time.Second)); err != nil {
				return
			}
		case <-stop:
			return
		}
	}
}

func readConsoleBackpressureFrames(client *websocket.Conn, messages chan<- ptyServerMessage, errs chan<- error, done chan<- struct{}) {
	defer close(done)
	defer close(messages)
	_ = client.SetReadDeadline(time.Now().Add(8 * time.Second))
	for {
		var message ptyServerMessage
		if err := client.ReadJSON(&message); err != nil {
			errs <- err
			return
		}
		messages <- message
		if message.Type == "exit" {
			return
		}
	}
}

func assertConsoleWriteTimeout(t *testing.T, err error) {
	t.Helper()
	var networkError net.Error
	if !errors.As(err, &networkError) || !networkError.Timeout() {
		t.Fatalf("expected bounded write timeout, got %v", err)
	}
}
