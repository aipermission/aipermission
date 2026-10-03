package maintenanceconsole

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/testkit/websocketpipe"
	"github.com/gorilla/websocket"
)

func TestMaintenanceTranscriptOverflowPreservesUTF8SnapshotAndReconnect(t *testing.T) {
	for _, character := range []string{"\u00f6", "\u20ac", "\U0001f680"} {
		for removed := 1; removed < len(character); removed++ {
			t.Run(fmt.Sprintf("%d-byte-cut-%d", len(character), removed), func(t *testing.T) {
				suffix := strings.Repeat("x", MaxTranscriptBytes-(len(character)-removed))
				verifyMaintenanceTranscriptReconnect(t, character, suffix, suffix)
			})
		}
	}
	t.Run("exact-boundary", func(t *testing.T) {
		suffix := "\U0001f680" + strings.Repeat("x", MaxTranscriptBytes-4)
		verifyMaintenanceTranscriptReconnect(t, "discard", suffix, suffix)
	})
}

func verifyMaintenanceTranscriptReconnect(t *testing.T, prefix, suffix, expected string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	session := &Session{pty: reader, status: "connected", shell: "fixture", clients: map[*websocket.Conn]*sync.Mutex{}}
	session.appendTranscriptAndClients(prefix)
	session.appendTranscriptAndClients(suffix)
	snapshot := session.Snapshot()
	if !utf8.ValidString(snapshot.Transcript) || len(snapshot.Transcript) > MaxTranscriptBytes || snapshot.Transcript != expected {
		t.Fatalf("overflow snapshot lost a UTF-8 boundary: valid=%t bytes=%d expected=%d", utf8.ValidString(snapshot.Transcript), len(snapshot.Transcript), len(expected))
	}
	server, client := websocketpipe.Pair(t)
	done := make(chan struct{})
	go func() { defer close(done); session.Attach(server) }()
	t.Cleanup(func() {
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("maintenance reconnect fixture did not detach")
		}
	})
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"snapshot", "ready"} {
		var message maintenanceConsoleServerMessage
		if err := client.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message.Type != kind || message.Status != "connected" {
			t.Fatalf("unexpected reconnect frame: %s/%s", message.Type, message.Status)
		}
		if kind == "snapshot" && (message.Data != expected || !utf8.ValidString(message.Data) || len(message.Data) > MaxTranscriptBytes) {
			t.Fatal("reconnect snapshot replaced or exceeded the retained UTF-8 transcript")
		}
	}
}
