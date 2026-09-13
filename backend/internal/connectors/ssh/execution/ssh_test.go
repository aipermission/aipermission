package execution

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type recordingCloser struct {
	closed bool
	name   string
	order  *[]string
}

func (closer *recordingCloser) Close() error {
	closer.closed = true
	if closer.order != nil {
		*closer.order = append(*closer.order, closer.name)
	}
	return nil
}

func TestStreamCommandValidatesCommandBeforeSSHSetup(t *testing.T) {
	_, err := StreamCommand(context.Background(), Target{}, "", nil, nil)
	if err == nil || err.Error() != "command is required" {
		t.Fatalf("expected command validation error, got %v", err)
	}
	_, err = RunCommand(context.Background(), Target{}, "")
	if err == nil || err.Error() != "command is required" {
		t.Fatalf("expected run command validation error, got %v", err)
	}
}

func TestActiveSSHResourcesRejectClientsPublishedAfterCancellation(t *testing.T) {
	resources := &activeSSHResources{}
	resources.close()
	candidate := &recordingCloser{}

	err := resources.publishClient(context.Background(), candidate)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("publish client error = %v, want context canceled", err)
	}
	if !candidate.closed {
		t.Fatal("client published after cancellation was not closed")
	}
}

func TestActiveSSHResourcesRejectSessionsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resources := &activeSSHResources{}
	candidate := &recordingCloser{}

	err := resources.publishSession(ctx, candidate)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("publish session error = %v, want context canceled", err)
	}
	if !candidate.closed {
		t.Fatal("session published after context cancellation was not closed")
	}
}

func TestActiveSSHResourcesCloseTransportBeforeSession(t *testing.T) {
	order := []string{}
	resources := &activeSSHResources{
		client:  &recordingCloser{name: "client", order: &order},
		session: &recordingCloser{name: "session", order: &order},
	}
	resources.close()
	if got := strings.Join(order, ","); got != "client,session" {
		t.Fatalf("close order = %q, want client,session", got)
	}
}

func TestDialClientContextClosesCanceledHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	peerClosed := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			peerClosed <- acceptErr
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		buffer := make([]byte, 256)
		if _, readErr := conn.Read(buffer); readErr != nil {
			peerClosed <- readErr
			return
		}
		_, readErr := conn.Read(buffer)
		peerClosed <- readErr
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, dialErr := DialClientContext(ctx, "tcp", listener.Addr().String(), &ssh.ClientConfig{
			User:            "test",
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Protocol fixture never reaches host-key verification.
			Timeout:         5 * time.Second,
		})
		done <- dialErr
	}()

	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case dialErr := <-done:
		if !errors.Is(dialErr, context.Canceled) {
			t.Fatalf("dial error = %v, want context canceled", dialErr)
		}
	case <-time.After(time.Second):
		t.Fatal("SSH handshake did not stop after cancellation")
	}
	select {
	case readErr := <-peerClosed:
		if readErr == nil {
			t.Fatal("peer read unexpectedly succeeded after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("SSH handshake socket remained open after cancellation")
	}
}

func TestStreamCommandRejectsInvalidPrivateKey(t *testing.T) {
	result, err := StreamCommand(context.Background(), Target{
		Host:       "127.0.0.1",
		Port:       22,
		Username:   "root",
		PrivateKey: "not a key",
	}, "ls", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "parse private key") {
		t.Fatalf("expected private key parsing to fail")
	}
	if result.DispatchStarted {
		t.Fatal("invalid private key was incorrectly marked as dispatched")
	}
}

func TestStreamWriterCopiesToBufferAndCallback(t *testing.T) {
	buffer := newLimitedBuffer(32)
	var callback []byte
	writer := streamWriter{
		buffer: buffer,
		fn: func(value []byte) {
			callback = append(callback, value...)
			value[0] = 'X'
		},
	}

	n, err := writer.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != 5 || buffer.String() != "hello" || string(callback) != "hello" {
		t.Fatalf("unexpected write result n=%d buffer=%q callback=%q", n, buffer.String(), string(callback))
	}
	n, err = writer.Write(nil)
	if err != nil || n != 0 {
		t.Fatalf("empty write should be a no-op, n=%d err=%v", n, err)
	}
}

func TestLimitedBufferTruncatesCapturedOutput(t *testing.T) {
	buffer := newLimitedBuffer(5)
	buffer.Write([]byte("hello"))
	buffer.Write([]byte(" world"))

	if got := buffer.String(); !strings.Contains(got, "hello") || !strings.Contains(got, "output truncated") {
		t.Fatalf("expected truncated output marker, got %q", got)
	}
}
