package apiadapter

import (
	"bytes"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
)

func TestStartupInputAfterConnect(t *testing.T) {
	if got := startupInputAfterConnect("q\n", false); got != "q\n" {
		t.Fatalf("normal session startup input = %q", got)
	}

	if got := startupInputAfterConnect("q\n", true); got != "" {
		t.Fatalf("environment session startup input = %q, want empty", got)
	}

	var input bytes.Buffer
	if err := writeEnvironmentBootstrapCommand(&input, "q", "bootstrap-command"); err != nil {
		t.Fatal(err)
	}
	if got := input.String(); got != "q\nbootstrap-command\n" {
		t.Fatalf("bootstrap input = %q", got)
	}
}

func TestSessionEnvironmentBootstrapOwnsItsProtocol(t *testing.T) {
	bootstrap, err := newSessionEnvironmentBootstrap(7)
	if err != nil {
		t.Fatal(err)
	}
	if bootstrap.protocol == nil {
		t.Fatal("bootstrap protocol is nil")
	}
	if command := bootstrap.Command(); command == "" {
		t.Fatal("bootstrap command is empty")
	}
}

func TestReadConsoleReturnsExactSessionHandle(t *testing.T) {
	handles := exactSessionActionHandles(console.Record{ID: 12, Generation: 34})
	if handles.SessionID != 12 || handles.SessionGeneration != 34 {
		t.Fatalf("read console handle = %#v", handles)
	}
}
