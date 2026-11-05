package sqlserverconnector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	mssql "github.com/microsoft/go-mssqldb"
)

func TestNativeExecutionDecoderFailureBeforeMetadataDoesNotWaitForDeadline(t *testing.T) {
	client, peer := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer peer.Close()
		_ = peer.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := readExecutionPacket(peer, 0x12); err != nil {
			return
		}
		if err := writeExecutionReply(peer, []byte{1, 0, 6, 0, 1, 0xff, 2}); err != nil {
			return
		}
		if _, err := readExecutionPacket(peer, 0x10); err != nil {
			return
		}
		ack := []byte{0xad, 10, 0, 1, 0x74, 0, 0, 4, 0, 0, 0, 0, 0}
		if err := writeExecutionReply(peer, append(ack, executionWireDone(0)...)); err != nil {
			return
		}
		if _, err := readExecutionPacket(peer, 1); err != nil {
			return
		}
		if err := writeExecutionReply(peer, executionWireDone(0)); err != nil {
			return
		}
		if _, err := readExecutionPacket(peer, 1); err != nil {
			return
		}
		// The complete packet is readable; only the native token decoder fails.
		if err := writeExecutionReply(peer, []byte{0}); err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, peer)
	}()
	t.Cleanup(func() { _ = client.Close(); _ = peer.Close(); <-done })
	runtime := fixtureRuntime()
	runtime.Target.Config["tls_mode"] = "disable"
	runtime.Capabilities = &executionWireTransport{Conn: client}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	action, err := New().PrepareAction(ctx, connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionExecuteSQL, Input: map[string]any{"sql": "SELECT 7;", "timeout_seconds": 300}})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result, err := New().ExecuteAction(ctx, runtime, action)
	if err != nil || result.Status != connectors.ResultOutcomeUnknown || !strings.Contains(result.Error, "unknown token type returned") || time.Since(started) >= time.Second || ctx.Err() != nil {
		t.Fatalf("terminal decoder failure waited for cancellation or lost classification: %#v %v", result, err)
	}
}

func TestOnlyOrdinaryServerErrorsContinueDrainingTheBatch(t *testing.T) {
	for _, tc := range []struct {
		err         error
		recoverable bool
	}{
		{mssql.Error{Class: 16, Number: 50000}, true},
		{fmt.Errorf("wrapped: %w", mssql.Error{Class: 16}), true},
		{mssql.Error{Class: 20}, false},
		{mssql.Error{Class: 16, All: []mssql.Error{{Class: 20}}}, false},
		{mssql.ServerError{}, false},
		{mssql.StreamError{InnerError: io.ErrUnexpectedEOF}, false},
		{errors.New("native decoder error"), false},
	} {
		if got := recoverableExecutionError(tc.err); got != tc.recoverable {
			t.Fatalf("%T recovery=%v, want %v", tc.err, got, tc.recoverable)
		}
	}
}
