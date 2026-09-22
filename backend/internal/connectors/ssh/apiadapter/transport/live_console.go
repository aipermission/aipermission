package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter/management"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sessionenvprotocol"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"golang.org/x/crypto/ssh"
)

type Transport struct{}

func (Transport) OpenLiveConsole(ctx context.Context, server connectorapi.LiveConsoleGateway, runtime connectorapi.LiveConsoleRuntime, request connectorapi.LiveConsoleOpenRequest) (*connectorapi.LiveConsoleSession, error) {
	gateway, err := management.PeerIdentityFrom(server)
	if err != nil {
		return nil, err
	}
	target, privateKey, err := management.TargetMaterialForRuntime(ctx, runtime, request.RuntimeID)
	if err != nil {
		return nil, fmt.Errorf("resolve ssh material: %w", err)
	}
	return openLiveConsoleWithMaterial(ctx, gateway, target, privateKey.PrivateKey, request.Rows, request.Cols, LiveConsoleOptions{
		Generation:        request.Generation,
		HasEnvironment:    request.HasEnvironment,
		ForceShellCommand: strings.TrimSpace(stringPayload(request.Params, "force_shell_command")),
	})
}

func (Transport) ExpectedLiveConsolePeerIdentities(ctx context.Context, server connectorapi.PeerIdentityGateway, runtime connectorapi.LiveConsoleRuntime, runtimeID int64) ([]string, error) {
	gateway, err := management.PeerIdentityFrom(server)
	if err != nil {
		return nil, err
	}
	target, _, err := management.TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("resolve ssh material: %w", err)
	}
	return execution.TrustedHostFingerprints(
		gateway.ConnectorTrustStorePath(),
		net.JoinHostPort(target.Host, strconv.Itoa(target.Port)),
	)
}

func openLiveConsoleWithMaterial(ctx context.Context, gateway connectorapi.PeerIdentityGateway, target management.TargetMaterial, privateKey string, rows int, cols int, options LiveConsoleOptions) (*connectorapi.LiveConsoleSession, error) {
	if strings.TrimSpace(options.ForceShellCommand) != "" {
		target.ForceShellCommand = strings.TrimSpace(options.ForceShellCommand)
	}
	if options.StartupInputAfterConnect != "" {
		target.StartupInputAfterConnect = options.StartupInputAfterConnect
	}
	hasEnvironment := options.HasEnvironment || (options.Environment != nil && options.Environment.Len() > 0)
	if hasEnvironment && target.ForceShellCommand != "" {
		return nil, errors.New("session environment is not supported with a forced shell command")
	}
	signer, err := ssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	hostKeyCallback, err := execution.HostKeyCallback(gateway.ConnectorTrustStorePath())
	if err != nil {
		return nil, fmt.Errorf("load known_hosts: %w", err)
	}
	peerIdentity := ""
	verifiedHostKeyCallback := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := hostKeyCallback(hostname, remote, key); err != nil {
			return err
		}
		peerIdentity = execution.HostKeyFingerprintSHA256(key)
		return nil
	}
	config := &ssh.ClientConfig{
		User:            target.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: verifiedHostKeyCallback,
		Timeout:         12 * time.Second,
	}
	address := net.JoinHostPort(target.Host, fmt.Sprintf("%d", target.Port))
	sshClient, err := execution.DialClientContext(ctx, "tcp", address, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}
	stopOpeningCancellation := context.AfterFunc(ctx, func() { _ = sshClient.Close() })
	defer stopOpeningCancellation()
	openingFailed := true
	defer func() {
		if openingFailed {
			_ = sshClient.Close()
		}
	}()
	sshSession, err := sshClient.NewSession()
	if err != nil {
		return nil, liveConsoleOpeningError(ctx, "new ssh session", err)
	}
	stdin, err := sshSession.StdinPipe()
	if err != nil {
		_ = sshSession.Close()
		_ = sshClient.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := sshSession.StdoutPipe()
	if err != nil {
		_ = sshSession.Close()
		_ = sshClient.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := sshSession.StderrPipe()
	if err != nil {
		_ = sshSession.Close()
		_ = sshClient.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if rows < 1 {
		rows = 32
	}
	if cols < 1 {
		cols = 120
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if hasEnvironment {
		modes[ssh.ECHO] = 0
	}
	if err := sshSession.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		return nil, liveConsoleOpeningError(ctx, "request pty", err)
	}
	closeProducer := func() error { return closeLiveConsoleProducer(sshSession, sshClient) }
	ownedOutput := newLiveConsoleOutput(sshSession.Wait, closeProducer)
	var applyEnvironment func(context.Context, connectorapi.SessionEnvironment) error
	if hasEnvironment {
		bootstrap, err := newSessionEnvironmentBootstrap(options.Generation)
		if err != nil {
			_ = sshSession.Close()
			_ = sshClient.Close()
			return nil, err
		}
		if err := sshSession.Shell(); err != nil {
			return nil, liveConsoleOpeningError(ctx, "start environment shell", err)
		}
		if err := writeEnvironmentBootstrapCommand(stdin, target.StartupInputAfterConnect, bootstrap.Command()); err != nil {
			_ = sshSession.Close()
			_ = sshClient.Close()
			return nil, err
		}
		applyEnvironment = func(applyCtx context.Context, environment connectorapi.SessionEnvironment) error {
			result, err := bootstrap.Apply(applyCtx, stdin, stdout, environment)
			if err != nil {
				return err
			}
			if !ownedOutput.Start(io.MultiReader(bytes.NewReader(result.Prelude), result.Reader), stderr) {
				return context.Canceled
			}
			return nil
		}
	} else if target.ForceShellCommand != "" {
		if err := sshSession.Start(target.ForceShellCommand); err != nil {
			return nil, liveConsoleOpeningError(ctx, "start forced shell command", err)
		}
	} else if err := sshSession.Shell(); err != nil {
		return nil, liveConsoleOpeningError(ctx, "start shell", err)
	}
	if !hasEnvironment {
		ownedOutput.Start(stdout, stderr)
	}
	if !stopOpeningCancellation() {
		_ = ownedOutput.Close()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		_ = ownedOutput.Close()
		return nil, err
	}
	openingFailed = false
	return &connectorapi.LiveConsoleSession{
		Stdin:                    stdin,
		Output:                   ownedOutput.output,
		Done:                     ownedOutput.done,
		Resize:                   func(cols int, rows int) error { return sshSession.WindowChange(rows, cols) },
		Close:                    ownedOutput.Close,
		PeerIdentity:             peerIdentity,
		StartupInputAfterConnect: startupInputAfterConnect(target.StartupInputAfterConnect, hasEnvironment),
		ApplyEnvironment:         applyEnvironment,
	}, nil
}

func liveConsoleOpeningError(ctx context.Context, operation string, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func closeLiveConsoleProducer(session io.Closer, client io.Closer) error {
	var closeErrors []error
	// Closing the client first interrupts blocked channel writes and reads.
	for _, closer := range []io.Closer{client, session} {
		if closer == nil {
			continue
		}
		if err := closer.Close(); err != nil && !errors.Is(err, io.EOF) {
			closeErrors = append(closeErrors, err)
		}
	}
	return errors.Join(closeErrors...)
}

type sessionEnvironmentBootstrap struct {
	protocol *sessionenvprotocol.Protocol
}

func newSessionEnvironmentBootstrap(generation int64) (sessionEnvironmentBootstrap, error) {
	protocol, err := sessionenvprotocol.New(generation)
	if err != nil {
		return sessionEnvironmentBootstrap{}, err
	}
	return sessionEnvironmentBootstrap{protocol: protocol}, nil
}

func (b sessionEnvironmentBootstrap) Command() string {
	if b.protocol == nil {
		return ""
	}
	return b.protocol.Command()
}

func (b sessionEnvironmentBootstrap) Apply(
	ctx context.Context,
	stdin io.Writer,
	stdout io.Reader,
	environment connectorapi.SessionEnvironment,
) (sessionenvprotocol.Result, error) {
	if b.protocol == nil {
		return sessionenvprotocol.Result{}, errors.New("session environment bootstrap is unavailable")
	}
	writeCloser, ok := stdin.(io.WriteCloser)
	if !ok {
		return sessionenvprotocol.Result{}, errors.New("session environment bootstrap requires a closable input stream")
	}
	return b.protocol.Bootstrap(ctx, writeCloser, stdout, environment)
}

func startupInputAfterConnect(input string, hasEnvironment bool) string {
	if hasEnvironment {
		return ""
	}
	return input
}

func writeEnvironmentBootstrapCommand(stdin io.Writer, startupInput string, command string) error {
	if startupInput != "" {
		if _, err := io.WriteString(stdin, startupInput); err != nil {
			return fmt.Errorf("write startup input before environment bootstrap: %w", err)
		}
		if !strings.HasSuffix(startupInput, "\n") && !strings.HasSuffix(startupInput, "\r") {
			if _, err := io.WriteString(stdin, "\n"); err != nil {
				return fmt.Errorf("separate startup input from environment bootstrap: %w", err)
			}
		}
	}
	if _, err := io.WriteString(stdin, command+"\n"); err != nil {
		return fmt.Errorf("write environment bootstrap command: %w", err)
	}
	return nil
}

func (Transport) DialConnectorTCP(ctx context.Context, server connectorapi.PeerIdentityGateway, runtime connectorapi.LiveConsoleRuntime, targetRef string, network string, address string) (net.Conn, error) {
	if network == "" {
		network = "tcp"
	}
	if network != "tcp" {
		return nil, fmt.Errorf("unsupported SSH connector transport network %q", network)
	}
	gateway, err := management.PeerIdentityFrom(server)
	if err != nil {
		return nil, err
	}
	runtimeID, err := management.RuntimeIDForTargetRef(ctx, runtime, targetRef)
	if err != nil {
		return nil, err
	}
	target, privateKey, err := management.TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("resolve ssh material: %w", err)
	}
	client, err := execution.DialSSH(ctx, management.ExecutionTarget(gateway, target, privateKey))
	if err != nil {
		return nil, err
	}
	type response struct {
		conn net.Conn
		err  error
	}
	done := make(chan response, 1)
	go func() {
		conn, err := client.Dial(network, address)
		done <- response{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = client.Close()
		go func() {
			value := <-done
			if value.conn != nil {
				_ = value.conn.Close()
			}
		}()
		return nil, ctx.Err()
	case value := <-done:
		if value.err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("ssh tcp dial: %w", value.err)
		}
		return sshTCPConn{Conn: value.conn, client: client}, nil
	}
}

type sshTCPConn struct {
	net.Conn
	client *ssh.Client
}

func (conn sshTCPConn) Close() error {
	connErr := conn.Conn.Close()
	clientErr := conn.client.Close()
	if connErr != nil {
		return connErr
	}
	return clientErr
}

func (Transport) RunConnectorCommand(ctx context.Context, server connectorapi.PeerIdentityGateway, runtime connectorapi.LiveConsoleRuntime, targetRef string, command string) (connectors.CommandRunResult, error) {
	gateway, err := management.PeerIdentityFrom(server)
	if err != nil {
		return connectors.CommandRunResult{}, err
	}
	runtimeID, err := management.RuntimeIDForTargetRef(ctx, runtime, targetRef)
	if err != nil {
		return connectors.CommandRunResult{}, err
	}
	target, privateKey, err := management.TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return connectors.CommandRunResult{}, fmt.Errorf("resolve ssh material: %w", err)
	}
	result, err := execution.RunCommand(ctx, management.ExecutionTarget(gateway, target, privateKey), command)
	return connectors.CommandRunResult{
		Stdout:          result.Stdout,
		Stderr:          result.Stderr,
		ExitCode:        result.ExitCode,
		DurationMS:      result.DurationMS,
		DispatchStarted: result.DispatchStarted,
	}, err
}
