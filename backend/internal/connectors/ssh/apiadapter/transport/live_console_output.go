package transport

import (
	"context"
	"io"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/console"
)

const liveConsoleOutputBufferSize = 4096

// liveConsoleOutput owns every goroutine reading from an SSH session. Closing
// the transport must unblock those readers and wait for them before returning.
type liveConsoleOutput struct {
	ctx           context.Context
	cancel        context.CancelFunc
	output        chan console.RuntimeOutput
	done          chan error
	wait          func() error
	closeProducer func() error

	mu         sync.Mutex
	started    bool
	closed     bool
	joined     chan struct{}
	closeOnce  sync.Once
	finishOnce sync.Once
	closeErr   error
}

func newLiveConsoleOutput(wait func() error, closeProducer func() error) *liveConsoleOutput {
	ctx, cancel := context.WithCancel(context.Background())
	return &liveConsoleOutput{
		ctx: ctx, cancel: cancel,
		output: make(chan console.RuntimeOutput, 16),
		done:   make(chan error, 1), joined: make(chan struct{}),
		wait: wait, closeProducer: closeProducer,
	}
}

func (output *liveConsoleOutput) Start(stdout io.Reader, stderr io.Reader) bool {
	output.mu.Lock()
	if output.closed || output.started {
		output.mu.Unlock()
		return false
	}
	output.started = true

	var readers sync.WaitGroup
	startReader := func(kind console.RuntimeOutputKind, reader io.Reader) {
		if reader == nil {
			return
		}
		readers.Add(1)
		go func() {
			defer readers.Done()
			pumpLiveConsoleOutput(output.ctx, reader, kind, output.output, make([]byte, liveConsoleOutputBufferSize))
		}()
	}
	startReader(console.RuntimeStdout, stdout)
	startReader(console.RuntimeStderr, stderr)

	go func() {
		var waitErr error
		if output.wait != nil {
			waitErr = output.wait()
		}
		_ = output.closeTransport()
		readers.Wait()
		output.finish(waitErr)
	}()
	output.mu.Unlock()
	return true
}

func (output *liveConsoleOutput) Close() error {
	output.mu.Lock()
	output.closed = true
	started := output.started
	output.mu.Unlock()
	output.cancel()
	closeErr := output.closeTransport()
	if !started {
		output.finish(context.Canceled)
	}
	<-output.joined
	return closeErr
}

func (output *liveConsoleOutput) closeTransport() error {
	output.closeOnce.Do(func() {
		if output.closeProducer != nil {
			output.closeErr = output.closeProducer()
		}
	})
	return output.closeErr
}

func (output *liveConsoleOutput) finish(err error) {
	output.finishOnce.Do(func() {
		close(output.output)
		output.done <- err
		close(output.done)
		close(output.joined)
	})
}

func pumpLiveConsoleOutput(ctx context.Context, reader io.Reader, kind console.RuntimeOutputKind, destination chan<- console.RuntimeOutput, buffer []byte) {
	defer clear(buffer)
	for {
		clear(buffer)
		n, err := reader.Read(buffer)
		if n > 0 {
			item := console.RuntimeOutput{Kind: kind, Data: string(buffer[:n])}
			clear(buffer[:n])
			select {
			case destination <- item:
			case <-ctx.Done():
				return
			}
		}
		if err != nil {
			// ssh.Session.Wait owns the terminal completion error. Reader errors
			// only terminate this stream so Done has one authoritative result.
			return
		}
	}
}
