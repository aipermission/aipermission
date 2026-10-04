package remotemetadata

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
)

func TestParseRemoteFileMetadataPreservesRootOwnershipAndPermissions(t *testing.T) {
	metadata, err := parse("gnu 81c0 0 0\n")
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.Mode.IsRegular() || metadata.Mode.Perm() != 0o700 || metadata.UID != 0 || metadata.GID != 0 {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestParseRemoteFileMetadataAcceptsBSDStatMode(t *testing.T) {
	metadata, err := parse("bsd 100640 501 20\n")
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.Mode.IsRegular() || metadata.Mode.Perm() != 0o640 || metadata.UID != 501 || metadata.GID != 20 {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestReadRemoteFileMetadataBoundsOutputAndHonorsCancellation(t *testing.T) {
	for _, output := range []string{"gnu 81c0 0 0\n", "bsd 100640 501 20"} {
		session := newFakeRemoteMetadataSession([][]byte{[]byte(output)}, false)
		if _, err := Read(t.Context(), session, "stat"); err != nil {
			t.Fatalf("valid metadata %q: %v", output, err)
		}
	}

	oversized := newFakeRemoteMetadataSession([][]byte{
		bytes.Repeat([]byte(" "), maxOutputBytes),
		[]byte("x"),
	}, false)
	if _, err := Read(t.Context(), oversized, "stat"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized metadata error = %v", err)
	}
	select {
	case <-oversized.closed:
	default:
		t.Fatal("oversized metadata did not close the SSH session")
	}

	ctx, cancel := context.WithCancel(t.Context())
	blocked := newFakeRemoteMetadataSession(nil, true)
	cancel()
	if _, err := Read(ctx, blocked, "stat"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled metadata error = %v", err)
	}
}

type fakeRemoteMetadataSession struct {
	chunks  [][]byte
	blocked bool
	stdout  io.Writer
	closed  chan struct{}
	once    sync.Once
}

func newFakeRemoteMetadataSession(chunks [][]byte, blocked bool) *fakeRemoteMetadataSession {
	return &fakeRemoteMetadataSession{chunks: chunks, blocked: blocked, closed: make(chan struct{})}
}

func (session *fakeRemoteMetadataSession) SetStdout(output io.Writer) { session.stdout = output }

func (session *fakeRemoteMetadataSession) Run(string) error {
	if session.blocked {
		<-session.closed
		return net.ErrClosed
	}
	for _, chunk := range session.chunks {
		if _, err := session.stdout.Write(chunk); err != nil {
			return err
		}
		select {
		case <-session.closed:
			return net.ErrClosed
		default:
		}
	}
	return nil
}

func (session *fakeRemoteMetadataSession) Close() error {
	session.once.Do(func() { close(session.closed) })
	return nil
}

func TestRemoteMetadataCommandSupportsGNUAndBSDStatWithoutOptionLikePaths(t *testing.T) {
	command := command("-private")
	if !strings.Contains(command, "stat -c") || !strings.Contains(command, "stat -f") || !strings.Contains(command, "'./-private'") {
		t.Fatalf("metadata command = %q", command)
	}
}

func TestQuoteRemoteShellArgEscapesPaths(t *testing.T) {
	if got := quoteShellArg("/tmp/user's file"); got != `'/tmp/user'\''s file'` {
		t.Fatalf("quoted path = %q", got)
	}
}
