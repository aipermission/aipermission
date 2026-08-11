package postgresconnector

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/jackc/pgx/v5"
)

func connect(ctx context.Context, runtime connectors.RuntimeContext) (*pgx.Conn, error) {
	username := strings.TrimSpace(publicString(runtime.Profile.Public, "username"))
	if username == "" {
		return nil, fmt.Errorf("%w: username", ErrMissingSecret)
	}
	if runtime.Secrets == nil {
		return nil, fmt.Errorf("%w: password", ErrMissingSecret)
	}
	password, err := runtime.Secrets.GetSecret(ctx, "password")
	if err != nil || strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("%w: password", ErrMissingSecret)
	}

	host := targetString(runtime.Target.Config, "host")
	database := targetString(runtime.Target.Config, "database")
	if host == "" {
		return nil, fmt.Errorf("%w: host is required", ErrInvalidConfig)
	}
	if database == "" {
		return nil, fmt.Errorf("%w: database is required", ErrInvalidConfig)
	}

	connURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(username, password),
		Host:   net.JoinHostPort(host, strconv.Itoa(targetPort(runtime.Target.Config))),
		Path:   "/" + database,
	}
	query := connURL.Query()
	query.Set("sslmode", sslMode(runtime.Target.Config))
	query.Set("connect_timeout", "10")
	connURL.RawQuery = query.Encode()

	config, err := pgx.ParseConfig(connURL.String())
	if err != nil {
		return nil, fmt.Errorf("parse postgres connection config: %w", err)
	}
	transport, err := postgresNetworkTransport(runtime)
	if err != nil {
		return nil, err
	}
	dialRequest := postgresNetworkDialRequest(runtime.Target)
	config.Config.DialFunc = func(ctx context.Context, network string, address string) (net.Conn, error) {
		return transport.DialConnectorTCP(ctx, dialRequest)
	}

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := configurePostgresSession(ctx, conn); err != nil {
		_ = conn.Close(ctx)
		return nil, err
	}
	return conn, nil
}

func configurePostgresSession(ctx context.Context, conn *pgx.Conn) error {
	if _, err := conn.Exec(ctx, `SELECT set_config('application_name', 'aipermission', false)`); err != nil {
		return fmt.Errorf("configure postgres application name: %w", err)
	}
	statementTimeout := strconv.Itoa(int(queryTimeout.Milliseconds())) + "ms"
	if _, err := conn.Exec(ctx, `SELECT set_config('statement_timeout', $1, false)`, statementTimeout); err != nil {
		return fmt.Errorf("configure postgres statement timeout: %w", err)
	}
	return nil
}

func postgresNetworkTransport(runtime connectors.RuntimeContext) (connectors.NetworkTransport, error) {
	transport, _ := runtime.Capability(connectors.NetworkTransportCapabilityName).(connectors.NetworkTransport)
	if transport == nil {
		return nil, ErrMissingTransport
	}
	return transport, nil
}

func postgresNetworkDialRequest(target connectors.TargetView) connectors.NetworkDialRequest {
	return connectors.NetworkDialRequest{
		SourceTargetRef:    target.Ref,
		SourceProjectID:    target.ProjectID,
		Mode:               connectionMode(target),
		Host:               targetString(target.Config, "host"),
		Port:               targetPort(target.Config),
		TransportTargetRef: strings.TrimSpace(targetString(target.Config, "transport_target_ref")),
	}
}

func startPostgresTunnel(ctx context.Context, runtime connectors.RuntimeContext) (string, int, func(), error) {
	transport, err := postgresNetworkTransport(runtime)
	if err != nil {
		return "", 0, nil, err
	}
	request := postgresNetworkDialRequest(runtime.Target)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", 0, nil, fmt.Errorf("start postgres local tunnel: %w", err)
	}
	tunnelCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			localConn, err := listener.Accept()
			if err != nil {
				return
			}
			go pipePostgresTunnelConn(tunnelCtx, transport, request, localConn)
		}
	}()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		cancel()
		_ = listener.Close()
		<-done
		return "", 0, nil, fmt.Errorf("postgres local tunnel address is not TCP")
	}
	cleanup := func() {
		cancel()
		_ = listener.Close()
		<-done
	}
	return "127.0.0.1", addr.Port, cleanup, nil
}

func pipePostgresTunnelConn(ctx context.Context, transport connectors.NetworkTransport, request connectors.NetworkDialRequest, localConn net.Conn) {
	remoteConn, err := transport.DialConnectorTCP(ctx, request)
	if err != nil {
		_ = localConn.Close()
		return
	}
	copyDone := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remoteConn, localConn)
		_ = remoteConn.Close()
		_ = localConn.Close()
		copyDone <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(localConn, remoteConn)
		_ = localConn.Close()
		_ = remoteConn.Close()
		copyDone <- struct{}{}
	}()
	<-copyDone
}
