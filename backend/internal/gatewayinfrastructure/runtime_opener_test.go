package gatewayinfrastructure

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

func TestConsoleRuntimeOpenerPreservesRequestAndSessionContract(t *testing.T) {
	wantError := errors.New("wait")
	wantEnvironment := func(context.Context, *sessionenv.Envelope) error { return nil }
	var received gatewayoperations.RuntimeOpenRequest
	adapted := gatewayoperations.AdaptRuntimeOpener(func(_ context.Context, request gatewayoperations.RuntimeOpenRequest) (*gatewayoperations.RuntimeSession, error) {
		received = request
		return &gatewayoperations.RuntimeSession{
			Stdin: nopWriteCloser{}, Stdout: strings.NewReader("stdout"), Stderr: strings.NewReader("stderr"),
			Wait: func() error { return wantError }, Resize: func(int, int) error { return nil },
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
	if err := session.Wait(); !errors.Is(err, wantError) {
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
