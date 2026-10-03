package console

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

func TestManagedConsolePreservesSplitUTF8InActualLiveFrames(t *testing.T) {
	for _, text := range []string{"\u00f6", "\u20ac", "\U0001f680", strings.Repeat("a", 4095) + "\u20ac"} {
		for split := max(1, len(text)-3); split < len(text); split++ {
			t.Run(fmt.Sprintf("bytes%d/split%d", len(text), split), func(t *testing.T) {
				live, snapshot := collectUTF8ConsoleFrames(t, func(session *managedConsoleSession) {
					session.appendOutput(text[:split])
					session.appendOutput(text[split:])
				})
				if live != text || snapshot != text {
					t.Fatalf("UTF-8 split lost: live bytes=%d snapshot bytes=%d expected bytes=%d", len(live), len(snapshot), len(text))
				}
			})
		}
	}
}

func TestManagedRuntimeEOFSeparatesKindsAndFlushesBeforeExit(t *testing.T) {
	live, snapshot := collectUTF8ConsoleFrames(t, func(session *managedConsoleSession) {
		output := make(chan RuntimeOutput, 4)
		output <- RuntimeOutput{Kind: RuntimeStdout, Data: "\xe2"}
		output <- RuntimeOutput{Kind: RuntimeStderr, Data: "stderr"}
		output <- RuntimeOutput{Kind: RuntimeStdout, Data: "\x82\xac"}
		output <- RuntimeOutput{Kind: RuntimeStderr, Data: "\xf0\x9f"}
		close(output)
		done := make(chan error)
		close(done)
		session.consumeRuntime(&RuntimeSession{Output: output, Done: done})
	})
	if expected := "stderr\u20ac\ufffd\ufffd"; live != expected || snapshot != expected {
		t.Fatalf("kind/EOF text differs: live=%q snapshot=%q", live, snapshot)
	}
}

func TestManagedRuntimeUnknownKindUsesStdoutCarry(t *testing.T) {
	live, snapshot := collectUTF8ConsoleFrames(t, func(session *managedConsoleSession) {
		output := make(chan RuntimeOutput, 2)
		output <- RuntimeOutput{Kind: RuntimeOutputKind(99), Data: "\xe2"}
		output <- RuntimeOutput{Kind: RuntimeStdout, Data: "\x82\xac"}
		close(output)
		done := make(chan error)
		close(done)
		session.consumeRuntime(&RuntimeSession{Output: output, Done: done})
	})
	if live != "\u20ac" || snapshot != "\u20ac" {
		t.Fatalf("non-stderr output lost stdout carry: live=%q snapshot=%q", live, snapshot)
	}
}

func collectUTF8ConsoleFrames(t *testing.T, produce func(*managedConsoleSession)) (string, string) {
	t.Helper()
	server, client := newConsoleFramePair(t)
	session := &managedConsoleSession{id: 7, ctx: t.Context(), status: "connected", manager: &Manager{db: &sql.DB{}}, workClosed: true, clients: map[*websocket.Conn]*sync.Mutex{server: {}}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		produce(session)
		session.closeExactRedactor()
		session.broadcast(ptyServerMessage{Type: "ready"})
	}()
	var live strings.Builder
	exited := false
	for {
		frame := readConsoleFrame(t, client)
		if frame.Type == "ready" {
			break
		}
		if frame.Type == "exit" {
			exited = true
			continue
		}
		if frame.Type != "output" || exited {
			t.Fatalf("unexpected or post-exit output frame: %#v", frame)
		}
		live.WriteString(frame.Data)
	}
	waitConsoleFrameWork(t, done)
	_, snapshot := session.snapshot()
	return live.String(), snapshot
}
