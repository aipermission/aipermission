package maintenanceconsole

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestMaintenanceClientAdmissionIsAtomicAndBounded(t *testing.T) {
	session := &Session{status: "connected", pty: &os.File{}, clients: map[*websocket.Conn]*sync.Mutex{}}
	var accepted, rejected atomic.Int32
	var group sync.WaitGroup
	for range 64 {
		group.Go(func() {
			_, err := session.registerClient(&websocket.Conn{}, &sync.Mutex{})
			if err == nil {
				accepted.Add(1)
			} else if errors.Is(err, errMaintenanceConsoleClientLimit) {
				rejected.Add(1)
			} else {
				t.Errorf("unexpected admission error: %v", err)
			}
		})
	}
	group.Wait()
	if accepted.Load() != maintenanceConsoleMaxClients || rejected.Load() != 64-maintenanceConsoleMaxClients || len(session.clients) != maintenanceConsoleMaxClients {
		t.Fatalf("admission counts accepted=%d rejected=%d registered=%d", accepted.Load(), rejected.Load(), len(session.clients))
	}
}

func TestMaintenanceInitialFrameFailureReleasesAdmission(t *testing.T) {
	for _, failedFrame := range []string{"snapshot", "ready"} {
		t.Run(failedFrame, func(t *testing.T) {
			session := &Session{status: "connected", pty: &os.File{}, clients: map[*websocket.Conn]*sync.Mutex{}}
			for range maintenanceConsoleMaxClients - 1 {
				if _, err := session.registerClient(&websocket.Conn{}, &sync.Mutex{}); err != nil {
					t.Fatal(err)
				}
			}
			err := session.initializeClient(&websocket.Conn{}, &sync.Mutex{}, func(frame maintenanceConsoleServerMessage) error {
				if frame.Type == failedFrame {
					return io.ErrClosedPipe
				}
				return nil
			})
			if !errors.Is(err, io.ErrClosedPipe) || len(session.clients) != maintenanceConsoleMaxClients-1 {
				t.Fatalf("failed %s retained admission: %v, clients=%d", failedFrame, err, len(session.clients))
			}
			if _, err := session.registerClient(&websocket.Conn{}, &sync.Mutex{}); err != nil {
				t.Fatalf("released slot unavailable: %v", err)
			}
		})
	}
}

func TestMaintenanceWebsocketLimitClosesOnlyExcessClientAndAllowsReconnect(t *testing.T) {
	session := &Session{status: "connected", pty: &os.File{}, clients: map[*websocket.Conn]*sync.Mutex{}}
	ended := make(chan struct{}, maintenanceConsoleMaxClients+2)
	var attachments sync.WaitGroup
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			attachments.Add(1)
			defer attachments.Done()
			session.Attach(ws)
			ended <- struct{}{}
		}
	}))
	defer server.Close()
	clients := []*websocket.Conn{}
	t.Cleanup(func() {
		for _, ws := range clients {
			_ = ws.Close()
		}
		done := make(chan struct{})
		go func() { attachments.Wait(); close(done) }()
		waitForMaintenanceSignal(t, done, "all websocket attachment handlers")
		session.mu.Lock()
		remaining := len(session.clients)
		session.mu.Unlock()
		if remaining != 0 {
			t.Errorf("websocket cleanup retained %d clients", remaining)
		}
	})
	dial := func() *websocket.Conn {
		t.Helper()
		ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, ws)
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		return ws
	}
	readInitial := func(ws *websocket.Conn) {
		t.Helper()
		for _, want := range []string{"snapshot", "ready"} {
			var frame maintenanceConsoleServerMessage
			if err := ws.ReadJSON(&frame); err != nil || frame.Type != want {
				t.Fatalf("initial frame=%#v, want=%s, error=%v", frame, want, err)
			}
		}
	}
	for range maintenanceConsoleMaxClients {
		readInitial(dial())
	}
	excess := dial()
	var rejected maintenanceConsoleServerMessage
	if err := excess.ReadJSON(&rejected); err != nil || rejected.Type != "error" || rejected.Status != "client_limit" {
		t.Fatalf("excess client response = %#v, %v", rejected, err)
	}
	if _, _, err := excess.ReadMessage(); err == nil {
		t.Fatal("excess websocket was not closed")
	}
	waitForMaintenanceSignal(t, ended, "excess attachment cleanup")
	_ = clients[0].Close()
	waitForMaintenanceSignal(t, ended, "accepted attachment cleanup")
	readInitial(dial())
	session.mu.Lock()
	count := len(session.clients)
	session.mu.Unlock()
	if count != maintenanceConsoleMaxClients {
		t.Fatalf("active clients after reconnect = %d", count)
	}
}
