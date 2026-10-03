package maintenanceconsole

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/testkit/websocketpipe"
	"github.com/gorilla/websocket"
)

func TestMaintenanceConsolePreservesSplitUTF8InActualReadLoop(t *testing.T) {
	for _, tc := range []struct {
		name, input, expected string
	}{
		{"split-buffer", strings.Repeat("a", 4095) + "\u00f6\u20ac\U0001f680", strings.Repeat("a", 4095) + "\u00f6\u20ac\U0001f680"},
		{"unfinished-eof", "visible \xf0\x9f", "visible \ufffd\ufffd"},
	} {
		t.Run(tc.name, func(t *testing.T) { verifyMaintenanceUTF8Frames(t, tc.input, tc.expected) })
	}
}

func verifyMaintenanceUTF8Frames(t *testing.T, input, expected string) {
	t.Helper()
	server, client := websocketpipe.Pair(t)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	session := &Session{pty: reader, shell: "fixture", status: "connected", clients: map[*websocket.Conn]*sync.Mutex{server: {}}, closed: make(chan struct{})}
	done := make(chan struct{})
	go func() { defer close(done); session.readLoop() }()
	go func() {
		_, _ = writer.Write([]byte(input))
		_ = writer.Close()
	}()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	var live strings.Builder
	for {
		var frame maintenanceConsoleServerMessage
		if err := client.ReadJSON(&frame); err != nil {
			break
		}
		if frame.Type == "output" {
			live.WriteString(frame.Data)
		}
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("maintenance fixture failed to close")
	}
	if live.String() != expected || session.transcript != live.String() {
		t.Fatalf("UTF-8 split lost: live bytes=%d snapshot bytes=%d expected bytes=%d", live.Len(), len(session.transcript), len(expected))
	}
}
