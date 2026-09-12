// Package gatewayoperations owns command-request, backup, observation,
// maintenance-console, transfer, and message operations for the gateway.
package gatewayoperations

import (
	"context"
	"io"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
	"github.com/gorilla/websocket"
)

type MaintenanceConsoleDescriptor struct {
	Supported          bool
	Shell              string
	MaxInputBytes      int
	MaxTranscriptBytes int
}

type MaintenanceConsoleSnapshot struct {
	Status     string
	Shell      string
	Transcript string
}

type MaintenanceConsoleRuntime interface {
	Descriptor() MaintenanceConsoleDescriptor
	Snapshot() (MaintenanceConsoleSnapshot, bool)
	Open() (MaintenanceConsoleSnapshot, error)
	Active() bool
	Attach(*websocket.Conn)
	Close() bool
}

type RuntimeOpenRequest struct {
	RuntimeID      int64
	Generation     int64
	Rows           int
	Cols           int
	Params         map[string]any
	HasEnvironment bool
}

type RuntimeOpener func(context.Context, RuntimeOpenRequest) (*RuntimeSession, error)

type RuntimeSession struct {
	Stdin                    io.WriteCloser
	Stdout                   io.Reader
	Stderr                   io.Reader
	Wait                     func() error
	Resize                   func(cols int, rows int) error
	Close                    func() error
	ApplyEnvironment         func(context.Context, *sessionenv.Envelope) error
	PeerIdentity             string
	StartupInputAfterConnect string
}

func AdaptRuntimeOpener(opener RuntimeOpener) console.RuntimeOpener {
	if opener == nil {
		return nil
	}
	return func(ctx context.Context, request console.RuntimeOpenRequest) (*console.RuntimeSession, error) {
		session, err := opener(ctx, RuntimeOpenRequest{
			RuntimeID: request.RuntimeID, Generation: request.Generation, Rows: request.Rows, Cols: request.Cols,
			Params: request.Params, HasEnvironment: request.HasEnvironment,
		})
		if err != nil || session == nil {
			return nil, err
		}
		return &console.RuntimeSession{
			Stdin: session.Stdin, Stdout: session.Stdout, Stderr: session.Stderr,
			Wait: session.Wait, Resize: session.Resize, Close: session.Close,
			ApplyEnvironment: session.ApplyEnvironment, PeerIdentity: session.PeerIdentity,
			StartupInputAfterConnect: session.StartupInputAfterConnect,
		}, nil
	}
}
