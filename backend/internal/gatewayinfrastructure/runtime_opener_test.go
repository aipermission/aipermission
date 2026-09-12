package gatewayinfrastructure

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func TestConsoleRuntimeOpenerPreservesRequestAndSessionContract(t *testing.T) {
	wantError := errors.New("wait")
	done := make(chan error, 1)
	done <- wantError
	close(done)
	output := make(chan console.RuntimeOutput, 2)
	output <- console.RuntimeOutput{Kind: console.RuntimeStdout, Data: "stdout"}
	output <- console.RuntimeOutput{Kind: console.RuntimeStderr, Data: "stderr"}
	close(output)
	wantEnvironment := func(context.Context, gatewayoperations.SessionEnvironment) error { return nil }
	var received gatewayoperations.RuntimeOpenRequest
	adapted := gatewayoperations.AdaptRuntimeOpener(func(_ context.Context, request gatewayoperations.RuntimeOpenRequest) (*gatewayoperations.RuntimeSession, error) {
		received = request
		return &gatewayoperations.RuntimeSession{
			Stdin: nopWriteCloser{}, Output: output,
			Done: done, Resize: func(int, int) error { return nil },
			Close: func() error { return nil }, ApplyEnvironment: wantEnvironment,
			PeerIdentity: "peer", StartupInputAfterConnect: "q",
		}, nil
	})

	session, err := adapted(t.Context(), console.RuntimeOpenRequest{
		RuntimeID: 7, Generation: 9, Rows: 31, Cols: 117,
		Params: map[string]any{"container": "api"}, HasEnvironment: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if received.RuntimeID != 7 || received.Generation != 9 || received.Rows != 31 || received.Cols != 117 || !received.HasEnvironment || received.Params["container"] != "api" {
		t.Fatalf("adapted request = %#v", received)
	}
	if session == nil || session.PeerIdentity != "peer" || session.StartupInputAfterConnect != "q" || session.ApplyEnvironment == nil {
		t.Fatalf("adapted session = %#v", session)
	}
	if event := <-session.Output; event.Kind != console.RuntimeStdout || event.Data != "stdout" {
		t.Fatalf("adapted output = %#v", event)
	}
	if event := <-session.Output; event.Kind != console.RuntimeStderr || event.Data != "stderr" {
		t.Fatalf("adapted output = %#v", event)
	}
	if err := <-session.Done; !errors.Is(err, wantError) {
		t.Fatalf("wait error = %v", err)
	}
}

func TestConsoleRuntimeOpenerPreservesNilAndFailure(t *testing.T) {
	if gatewayoperations.AdaptRuntimeOpener(nil) != nil {
		t.Fatal("nil gateway opener became callable")
	}
	want := errors.New("open failed")
	adapted := gatewayoperations.AdaptRuntimeOpener(func(context.Context, gatewayoperations.RuntimeOpenRequest) (*gatewayoperations.RuntimeSession, error) {
		return nil, want
	})
	if session, err := adapted(t.Context(), console.RuntimeOpenRequest{}); session != nil || !errors.Is(err, want) {
		t.Fatalf("session=%#v err=%v", session, err)
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Write(value []byte) (int, error) { return len(value), nil }
func (nopWriteCloser) Close() error                    { return nil }
