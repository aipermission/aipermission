package execution

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
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
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
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
	var mu sync.Mutex
	var client *ssh.Client
	var session *ssh.Session
	closeActive := func() {
		mu.Lock()
		defer mu.Unlock()
		if session != nil {
			_ = session.Close()
		}
		if client != nil {
			_ = client.Close()
		}
	}

	go func() {
		sshClient, err := DialSSH(ctx, target)
		if err != nil {
			done <- response{err: err}
			return
		}
		mu.Lock()
		client = sshClient
		mu.Unlock()
		defer sshClient.Close()

		sshSession, err := sshClient.NewSession()
		if err != nil {
			done <- response{err: fmt.Errorf("new ssh session: %w", err)}
			return
		}
		if err := ctx.Err(); err != nil {
			_ = sshSession.Close()
			done <- response{err: err}
			return
		}
		mu.Lock()
		session = sshSession
		mu.Unlock()
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
				Stdout:     stdout.String(),
				Stderr:     stderr.String(),
				ExitCode:   exitCode,
				DurationMS: time.Since(started).Milliseconds(),
			},
			err: runErr,
		}
	}()

	select {
	case <-ctx.Done():
		closeActive()
		return Result{}, ctx.Err()
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

	type response struct {
		client *ssh.Client
		err    error
	}
	done := make(chan response, 1)
	go func() {
		client, err := ssh.Dial("tcp", address, config)
		if err != nil {
			done <- response{err: fmt.Errorf("ssh dial: %w", err)}
			return
		}
		if err := ctx.Err(); err != nil {
			_ = client.Close()
			done <- response{err: err}
			return
		}
		done <- response{client: client}
	}()

	select {
	case <-ctx.Done():
		go func() {
			value := <-done
			if value.client != nil {
				_ = value.client.Close()
			}
		}()
		return nil, ctx.Err()
	case value := <-done:
		return value.client, value.err
	}
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
