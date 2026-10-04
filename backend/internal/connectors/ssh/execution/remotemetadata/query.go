package remotemetadata

import (
	"context"
	"fmt"
	"io"
)

const maxOutputBytes = 4 << 10

// Session is the authenticated command session supplied by the transport owner.
type Session interface {
	SetStdout(io.Writer)
	Run(string) error
	Close() error
}

// Read consumes complete stat metadata from a caller-authenticated session.
// Close must unblock Run; this function owns the session and drains Run before
// returning. It does not decide whether a transfer may commit or replace a file.
func Read(ctx context.Context, session Session, remotePath string) (Metadata, error) {
	defer session.Close()
	output := newBoundedOutput(maxOutputBytes, session.Close)
	session.SetStdout(output)
	done := make(chan error, 1)
	go func() { done <- session.Run(command(remotePath)) }()

	var runErr error
	select {
	case runErr = <-done:
	case <-ctx.Done():
		_ = session.Close()
		<-done
		return Metadata{}, ctx.Err()
	}
	if output.Exceeded() {
		return Metadata{}, fmt.Errorf("read complete remote metadata: response exceeds %d bytes", maxOutputBytes)
	}
	if runErr != nil {
		return Metadata{}, fmt.Errorf("read complete remote metadata: %w", runErr)
	}
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	return parse(output.String())
}
