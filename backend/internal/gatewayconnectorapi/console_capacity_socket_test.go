package gatewayconnectorapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestCapacitySocketOnlySignalsAndRetiresOnDisconnect(t *testing.T) {
	sessions := &capacityTestSessions{}
	runtime := testRuntime(t, &sessions.fakeSessions)
	runtime.Sessions = sessions
	upgrader := websocket.Upgrader{}
	runtime.UpgradeWebSocket = func(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
		return upgrader.Upgrade(w, r, nil)
	}
	handlers := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(done); handlers.CapacityAttach(w, r) }))
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(time.Second))
	var event map[string]any
	if err := ws.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	if len(event) != 1 || event["revision"] != float64(1) {
		t.Fatalf("capacity event exposed metadata: %+v", event)
	}
	_ = ws.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("capacity watcher survived disconnect")
	}
}
