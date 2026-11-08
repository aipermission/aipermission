package gatewayconnectorapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type updatingCapacitySessions struct{ capacityTestSessions }

func (sessions *updatingCapacitySessions) WaitCapacity(ctx context.Context, after uint64) (uint64, error) {
	timer := time.NewTimer(5 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return after + 1, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func TestCapacityHeartbeatSurvivesContinuousRevisionUpdates(t *testing.T) {
	sessions := &updatingCapacitySessions{}
	runtime := testRuntime(t, &sessions.fakeSessions)
	runtime.Sessions = sessions
	upgrader := websocket.Upgrader{}
	runtime.UpgradeWebSocket = func(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
		return upgrader.Upgrade(w, r, nil)
	}
	handlers := NewLiveConsoleHTTPHandlers(func(http.ResponseWriter) (*LiveConsoleHTTPRuntime, bool) { return runtime, true })
	server := httptest.NewServer(http.HandlerFunc(handlers.CapacityAttach))
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var ping atomic.Bool
	ws.SetPingHandler(func(data string) error {
		ping.Store(true)
		return ws.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})
	_ = ws.SetReadDeadline(time.Now().Add(25 * time.Second))
	for !ping.Load() {
		if _, _, err := ws.ReadMessage(); err != nil {
			t.Fatalf("updates suppressed heartbeat: %v", err)
		}
	}
}
