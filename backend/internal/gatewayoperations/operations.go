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

type SessionEnvironment interface {
	Len() int
	ForEach(func(name string, value []byte, replaceExisting bool, itemID int64, valueVersion int64, sourceProjectID int64) error) error
}

type RuntimeSession struct {
	Stdin                    io.WriteCloser
	Output                   <-chan console.RuntimeOutput
	Done                     <-chan error
	Resize                   func(cols int, rows int) error
	Close                    func() error
	ApplyEnvironment         func(context.Context, SessionEnvironment) error
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
		var applyEnvironment func(context.Context, *sessionenv.Envelope) error
		if session.ApplyEnvironment != nil {
			applyEnvironment = func(applyCtx context.Context, environment *sessionenv.Envelope) error {
				return session.ApplyEnvironment(applyCtx, environment)
			}
		}
		return &console.RuntimeSession{
			Stdin: session.Stdin, Output: session.Output,
			Done: session.Done, Resize: session.Resize, Close: session.Close,
			ApplyEnvironment: applyEnvironment, PeerIdentity: session.PeerIdentity,
			StartupInputAfterConnect: session.StartupInputAfterConnect,
		}, nil
	}
}
