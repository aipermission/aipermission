package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

const maxCapturedOutputBytes = 1 << 20

type Target struct {
	Host           string
	Port           int
	Username       string
	PrivateKey     string
	KnownHostsPath string
}

type Result struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMS      int64  `json:"duration_ms"`
	DispatchStarted bool   `json:"-"`
}

type activeSSHResources struct {
	mu       sync.Mutex
	canceled bool
	client   io.Closer
	session  io.Closer
}

func (resources *activeSSHResources) publishClient(ctx context.Context, client io.Closer) error {
	resources.mu.Lock()
	if err := resources.cancellationErr(ctx); err != nil {
		resources.mu.Unlock()
		_ = client.Close()
		return err
	}
	resources.client = client
	resources.mu.Unlock()
	return nil
}

func (resources *activeSSHResources) publishSession(ctx context.Context, session io.Closer) error {
	resources.mu.Lock()
	if err := resources.cancellationErr(ctx); err != nil {
		resources.mu.Unlock()
		_ = session.Close()
		return err
	}
	resources.session = session
	resources.mu.Unlock()
	return nil
}

func (resources *activeSSHResources) cancellationErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if resources.canceled {
		return context.Canceled
	}
	return nil
}

func (resources *activeSSHResources) close() {
	resources.mu.Lock()
	resources.canceled = true
	client := resources.client
	session := resources.session
	resources.client = nil
	resources.session = nil
	resources.mu.Unlock()

	if client != nil {
		_ = client.Close()
	}
	if session != nil {
		_ = session.Close()
	}
}

func RunCommand(ctx context.Context, target Target, command string) (Result, error) {
	return StreamCommand(ctx, target, command, nil, nil)
}

func StreamCommand(ctx context.Context, target Target, command string, onStdout func([]byte), onStderr func([]byte)) (Result, error) {
	if command == "" {
		return Result{}, fmt.Errorf("command is required")
	}

	started := time.Now()

	type response struct {
		result Result
		err    error
	}
	done := make(chan response, 1)
	var dispatchStarted atomic.Bool
	resources := &activeSSHResources{}

	go func() {
		sshClient, err := DialSSH(ctx, target)
		if err != nil {
			done <- response{err: err}
			return
		}
		if err := resources.publishClient(ctx, sshClient); err != nil {
			done <- response{err: err}
			return
		}
		defer sshClient.Close()

		sshSession, err := sshClient.NewSession()
		if err != nil {
			done <- response{err: fmt.Errorf("new ssh session: %w", err)}
			return
		}
		if err := resources.publishSession(ctx, sshSession); err != nil {
			done <- response{err: err}
			return
		}
		defer sshSession.Close()

		stdout := newLimitedBuffer(maxCapturedOutputBytes)
		stderr := newLimitedBuffer(maxCapturedOutputBytes)
		sshSession.Stdout = streamWriter{
			buffer: stdout,
			fn:     onStdout,
		}
		sshSession.Stderr = streamWriter{
			buffer: stderr,
			fn:     onStderr,
		}

		dispatchStarted.Store(true)
		err = sshSession.Run(command)
		exitCode := 0
		var runErr error
		if err != nil {
			exitCode = 1
			var exitErr *ssh.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitStatus()
			} else {
				runErr = fmt.Errorf("run command: %w", err)
			}
		}

		done <- response{
			result: Result{
				Stdout:          stdout.String(),
				Stderr:          stderr.String(),
				ExitCode:        exitCode,
				DurationMS:      time.Since(started).Milliseconds(),
				DispatchStarted: true,
			},
			err: runErr,
		}
	}()

	select {
	case <-ctx.Done():
		resources.close()
		return Result{DurationMS: time.Since(started).Milliseconds(), DispatchStarted: dispatchStarted.Load()}, ctx.Err()
	case value := <-done:
		return value.result, value.err
	}
}

func DialSSH(ctx context.Context, target Target) (*ssh.Client, error) {
	signer, err := ssh.ParsePrivateKey([]byte(target.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	hostKeyCallback, err := HostKeyCallback(target.KnownHostsPath)
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:            target.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         12 * time.Second,
	}
	address := net.JoinHostPort(target.Host, fmt.Sprintf("%d", target.Port))
	client, err := DialClientContext(ctx, "tcp", address, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}
	return client, nil
}

// DialClientContext keeps ownership of the raw connection until the SSH
// handshake completes, so cancellation and handshake timeouts close the
// socket instead of leaving an unobservable ssh.Dial goroutine behind.
func DialClientContext(ctx context.Context, network, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if config == nil {
		return nil, errors.New("ssh client config is required")
	}

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout}
	raw, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}

	handshakeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if deadline, ok := handshakeCtx.Deadline(); ok {
		if err := raw.SetDeadline(deadline); err != nil {
			_ = raw.Close()
			return nil, fmt.Errorf("set ssh handshake deadline: %w", err)
		}
	}
	stopCancellation := context.AfterFunc(handshakeCtx, func() { _ = raw.Close() })
	connection, channels, requests, err := ssh.NewClientConn(raw, address, config)
	stopped := stopCancellation()
	if err != nil {
		_ = raw.Close()
		if contextErr := handshakeCtx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, err
	}
	if !stopped {
		_ = connection.Close()
		if contextErr := handshakeCtx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, context.Canceled
	}
	if err := raw.SetDeadline(time.Time{}); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("clear ssh handshake deadline: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = connection.Close()
		return nil, err
	}
	return ssh.NewClient(connection, channels, requests), nil
}

type streamWriter struct {
	buffer *limitedBuffer
	fn     func([]byte)
}

func (w streamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if w.buffer != nil {
		w.buffer.Write(p)
	}
	if w.fn != nil {
		cp := append([]byte(nil), p...)
		w.fn(cp)
	}
	return len(p), nil
}

type limitedBuffer struct {
	data      []byte
	limit     int
	truncated bool
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(p []byte) {
	if b.limit <= 0 || len(p) == 0 {
		return
	}
	remaining := b.limit - len(b.data)
	if remaining <= 0 {
		b.truncated = true
		return
	}
	if len(p) > remaining {
		b.data = append(b.data, p[:remaining]...)
		b.truncated = true
		return
	}
	b.data = append(b.data, p...)
}

func (b *limitedBuffer) String() string {
	if b == nil {
		return ""
	}
	value := string(b.data)
	if b.truncated {
		value += "\n[output truncated by aipermission]\n"
	}
	return value
}
