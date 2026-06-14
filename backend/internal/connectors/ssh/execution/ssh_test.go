package execution

import (
	"context"
	"strings"
	"testing"
)

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

func TestStreamCommandRejectsInvalidPrivateKey(t *testing.T) {
	_, err := StreamCommand(context.Background(), Target{
		Host:       "127.0.0.1",
		Port:       22,
		Username:   "root",
		PrivateKey: "not a key",
	}, "ls", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "parse private key") {
		t.Fatalf("expected private key parsing to fail")
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
