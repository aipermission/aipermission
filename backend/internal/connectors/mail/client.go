package mailconnector

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

const (
	connectPhaseTimeout = 10 * time.Second
	commandTimeout      = 15 * time.Second
	readActionTimeout   = 30 * time.Second
	smtpActionTimeout   = 45 * time.Second
)

type protocolSecrets struct {
	IMAPUsername string
	IMAPPassword string
	SMTPUsername string
	SMTPPassword string
}

type boundedReadConn struct {
	net.Conn
	read int64
}

type contextBoundConn struct {
	net.Conn
	stop func() bool
}

// deadlineCapConn keeps protocol libraries from extending or clearing the
// absolute connection/bootstrap deadline while authentication is in progress.
type deadlineCapConn struct {
	net.Conn
	mu       sync.RWMutex
	deadline time.Time
}

func newDeadlineCapConn(conn net.Conn, deadline time.Time) *deadlineCapConn {
	return &deadlineCapConn{Conn: conn, deadline: deadline}
}

func (conn *deadlineCapConn) release() {
	conn.mu.Lock()
	conn.deadline = time.Time{}
	conn.mu.Unlock()
}

func (conn *deadlineCapConn) capped(deadline time.Time) time.Time {
	conn.mu.RLock()
	cap := conn.deadline
	conn.mu.RUnlock()
	if !cap.IsZero() && (deadline.IsZero() || deadline.After(cap)) {
		return cap
	}
	return deadline
}

func (conn *deadlineCapConn) SetDeadline(deadline time.Time) error {
	return conn.Conn.SetDeadline(conn.capped(deadline))
}

func (conn *deadlineCapConn) SetReadDeadline(deadline time.Time) error {
	return conn.Conn.SetReadDeadline(conn.capped(deadline))
}

func (conn *deadlineCapConn) SetWriteDeadline(deadline time.Time) error {
	return conn.Conn.SetWriteDeadline(conn.capped(deadline))
}

func bindConnToContext(ctx context.Context, conn net.Conn) net.Conn {
	bound := &contextBoundConn{Conn: conn}
	bound.stop = context.AfterFunc(ctx, func() { _ = conn.Close() })
	return bound
}

func (conn *contextBoundConn) Close() error {
	if conn.stop != nil {
		conn.stop()
	}
	return conn.Conn.Close()
}

func (conn *boundedReadConn) Read(buffer []byte) (int, error) {
	remaining := int64(maxProtocolReadBytes) - conn.read
	if remaining <= 0 {
		var probe [1]byte
		count, err := conn.Conn.Read(probe[:])
		if count > 0 {
			return 0, ErrResponseTooLarge
		}
		if err == nil {
			return 0, io.ErrNoProgress
		}
		return 0, err
	}
	if int64(len(buffer)) > remaining {
		buffer = buffer[:remaining]
	}
	count, err := conn.Conn.Read(buffer)
	conn.read += int64(count)
	return count, err
}

func loadIMAPSecrets(ctx context.Context, runtime connectors.RuntimeContext) (protocolSecrets, error) {
	if runtime.Secrets == nil {
		return protocolSecrets{}, ErrMissingSecret
	}
	var result protocolSecrets
	var err error
	result.IMAPUsername, err = runtime.Secrets.GetSecret(ctx, "imap_username")
	if err != nil || strings.TrimSpace(result.IMAPUsername) == "" {
		return protocolSecrets{}, fmt.Errorf("%w: IMAP username", ErrMissingSecret)
	}
	result.IMAPPassword, err = runtime.Secrets.GetSecret(ctx, "imap_password")
	if err != nil || result.IMAPPassword == "" {
		return protocolSecrets{}, fmt.Errorf("%w: IMAP password", ErrMissingSecret)
	}
	return result, nil
}

func loadSMTPSecrets(ctx context.Context, runtime connectors.RuntimeContext, profile profileConfig) (protocolSecrets, error) {
	if runtime.Secrets == nil {
		return protocolSecrets{}, ErrMissingSecret
	}
	var result protocolSecrets
	if profile.SMTPAuthMode == "reuse_imap" {
		imapSecrets, err := loadIMAPSecrets(ctx, runtime)
		if err != nil {
			return protocolSecrets{}, err
		}
		result.SMTPUsername = imapSecrets.IMAPUsername
		result.SMTPPassword = imapSecrets.IMAPPassword
	}
	if profile.SMTPAuthMode == "separate" {
		var err error
		result.SMTPUsername, err = runtime.Secrets.GetSecret(ctx, "smtp_username")
		if err != nil || strings.TrimSpace(result.SMTPUsername) == "" {
			return protocolSecrets{}, fmt.Errorf("%w: SMTP username", ErrMissingSecret)
		}
		result.SMTPPassword, err = runtime.Secrets.GetSecret(ctx, "smtp_password")
		if err != nil || result.SMTPPassword == "" {
			return protocolSecrets{}, fmt.Errorf("%w: SMTP password", ErrMissingSecret)
		}
	}
	if profile.SMTPAuthMode == "disabled" {
		return protocolSecrets{}, fmt.Errorf("%w: SMTP is disabled", ErrInvalidConfig)
	}
	return result, nil
}

func dialProtocol(lifetimeCtx context.Context, dialCtx context.Context, runtime connectors.RuntimeContext, config targetConfig, host string, port int) (net.Conn, error) {
	transport, ok := runtime.Capability(connectors.NetworkTransportCapabilityName).(connectors.NetworkTransport)
	if !ok || transport == nil {
		return nil, ErrMissingTransport
	}
	conn, err := transport.DialConnectorTCP(dialCtx, connectors.NetworkDialRequest{
		SourceTargetRef:    runtime.Target.Ref,
		SourceProjectID:    runtime.Target.ProjectID,
		Mode:               config.ConnectionMode,
		Host:               host,
		Port:               port,
		TransportTargetRef: config.TransportTargetRef,
	})
	if err != nil {
		return nil, classifyProtocolError("connect", err)
	}
	return bindConnToContext(lifetimeCtx, &boundedReadConn{Conn: conn}), nil
}

func setConnDeadlineFromContext(conn net.Conn, ctx context.Context, fallback time.Duration) error {
	deadline := time.Now().Add(fallback)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	return conn.SetDeadline(deadline)
}

type tlsConfigFactory func(host string) *tls.Config

func openIMAP(ctx context.Context, runtime connectors.RuntimeContext, config targetConfig, profile profileConfig, secrets protocolSecrets) (*client.Client, error) {
	return openIMAPWithTLSConfig(ctx, runtime, config, profile, secrets, tlsConfigFor)
}

func openIMAPWithTLSConfig(ctx context.Context, runtime connectors.RuntimeContext, config targetConfig, profile profileConfig, secrets protocolSecrets, tlsConfig tlsConfigFactory) (*client.Client, error) {
	if !profile.IMAPEnabled {
		return nil, fmt.Errorf("%w: IMAP is disabled for this profile", ErrInvalidConfig)
	}
	bootstrapCtx, cancelBootstrap := context.WithTimeout(ctx, connectPhaseTimeout)
	defer cancelBootstrap()
	conn, err := dialProtocol(ctx, bootstrapCtx, runtime, config, config.IMAPHost, config.IMAPPort)
	if err != nil {
		return nil, err
	}
	bootstrapDeadline, _ := bootstrapCtx.Deadline()
	bootstrapConn := newDeadlineCapConn(conn, bootstrapDeadline)
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = conn.Close()
		}
	}()
	if err := setConnDeadlineFromContext(bootstrapConn, bootstrapCtx, connectPhaseTimeout); err != nil {
		return nil, classifyProtocolError("deadline", err)
	}
	var imapClient *client.Client
	verifiedTLS := false
	failurePhase := "IMAP greeting"
	if config.IMAPTLSMode == "implicit_tls" {
		tlsConn := tls.Client(bootstrapConn, tlsConfig(config.IMAPHost))
		if err := tlsConn.HandshakeContext(bootstrapCtx); err != nil {
			return nil, classifyProtocolError("TLS handshake", err)
		}
		verifiedTLS = true
		imapClient, err = client.New(tlsConn)
	} else {
		imapClient, err = client.New(bootstrapConn)
		if err == nil {
			imapClient.ErrorLog = log.New(io.Discard, "", 0)
			imapClient.Timeout = connectPhaseTimeout
			failurePhase = "IMAP STARTTLS"
			var supported bool
			supported, err = imapClient.SupportStartTLS()
			if err == nil && !supported {
				err = errors.New("server does not advertise STARTTLS")
			}
			if err == nil {
				err = imapClient.StartTLS(tlsConfig(config.IMAPHost))
				verifiedTLS = err == nil && imapClient.IsTLS()
			}
			if err == nil {
				// Force a post-TLS CAPABILITY exchange. Authentication and later
				// feature checks must never rely on cleartext advertisements.
				failurePhase = "IMAP post-TLS capability"
				_, err = imapClient.Capability()
			}
		}
	}
	if err != nil {
		return nil, classifyProtocolError(failurePhase, err)
	}
	imapClient.ErrorLog = log.New(io.Discard, "", 0)
	imapClient.Timeout = commandTimeout
	if err := setConnDeadlineFromContext(bootstrapConn, bootstrapCtx, connectPhaseTimeout); err != nil {
		_ = imapClient.Terminate()
		return nil, classifyProtocolError("deadline", err)
	}
	if !verifiedTLS {
		_ = imapClient.Terminate()
		return nil, fmt.Errorf("IMAP authentication refused because verified TLS is not active")
	}
	if err := imapClient.Login(secrets.IMAPUsername, secrets.IMAPPassword); err != nil {
		_ = imapClient.Terminate()
		return nil, classifyProtocolError("IMAP authentication", err)
	}
	bootstrapConn.release()
	_ = bootstrapConn.SetDeadline(time.Time{})
	closeOnError = false
	return imapClient, nil
}

func closeIMAP(client *client.Client) {
	if client == nil {
		return
	}
	if err := client.Logout(); err != nil {
		_ = client.Terminate()
	}
}

func openSMTP(ctx context.Context, runtime connectors.RuntimeContext, config targetConfig, profile profileConfig, secrets protocolSecrets) (*smtp.Client, error) {
	return openSMTPWithTLSConfig(ctx, runtime, config, profile, secrets, tlsConfigFor)
}

func openSMTPWithTLSConfig(ctx context.Context, runtime connectors.RuntimeContext, config targetConfig, profile profileConfig, secrets protocolSecrets, tlsConfig tlsConfigFactory) (*smtp.Client, error) {
	if profile.SMTPAuthMode == "disabled" {
		return nil, fmt.Errorf("%w: SMTP is disabled for this profile", ErrInvalidConfig)
	}
	bootstrapCtx, cancelBootstrap := context.WithTimeout(ctx, connectPhaseTimeout)
	defer cancelBootstrap()
	conn, err := dialProtocol(ctx, bootstrapCtx, runtime, config, config.SMTPHost, config.SMTPPort)
	if err != nil {
		return nil, err
	}
	bootstrapDeadline, _ := bootstrapCtx.Deadline()
	bootstrapConn := newDeadlineCapConn(conn, bootstrapDeadline)
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = conn.Close()
		}
	}()
	if err := setConnDeadlineFromContext(bootstrapConn, bootstrapCtx, connectPhaseTimeout); err != nil {
		return nil, classifyProtocolError("deadline", err)
	}
	var smtpClient *smtp.Client
	if config.SMTPTLSMode == "implicit_tls" {
		tlsConn := tls.Client(bootstrapConn, tlsConfig(config.SMTPHost))
		if err := tlsConn.HandshakeContext(bootstrapCtx); err != nil {
			return nil, classifyProtocolError("TLS handshake", err)
		}
		smtpClient = smtp.NewClient(tlsConn)
	} else {
		smtpClient, err = smtp.NewClientStartTLS(bootstrapConn, tlsConfig(config.SMTPHost))
		if err != nil {
			return nil, classifyProtocolError("SMTP STARTTLS", err)
		}
	}
	smtpClient.CommandTimeout = commandTimeout
	smtpClient.SubmissionTimeout = smtpActionTimeout
	if _, ok := smtpClient.TLSConnectionState(); !ok {
		_ = smtpClient.Close()
		return nil, fmt.Errorf("SMTP authentication refused because verified TLS is not active")
	}
	auth, err := smtpPasswordAuth(smtpClient, secrets.SMTPUsername, secrets.SMTPPassword)
	if err != nil {
		_ = smtpClient.Close()
		return nil, err
	}
	if err := smtpClient.Auth(auth); err != nil {
		_ = smtpClient.Close()
		return nil, classifyProtocolError("SMTP authentication", err)
	}
	bootstrapConn.release()
	if err := setConnDeadlineFromContext(bootstrapConn, ctx, smtpActionTimeout); err != nil {
		_ = smtpClient.Close()
		return nil, classifyProtocolError("SMTP action deadline", err)
	}
	closeOnError = false
	return smtpClient, nil
}

type smtpAuthCapabilities interface {
	SupportsAuth(mechanism string) bool
}

func smtpPasswordAuth(capabilities smtpAuthCapabilities, username, password string) (sasl.Client, error) {
	if capabilities.SupportsAuth(sasl.Plain) {
		return sasl.NewPlainClient("", username, password), nil
	}
	if capabilities.SupportsAuth(sasl.Login) {
		return sasl.NewLoginClient(username, password), nil
	}
	return nil, fmt.Errorf("SMTP server does not advertise supported password authentication")
}

func closeSMTP(client *smtp.Client) {
	if client == nil {
		return
	}
	if err := client.Quit(); err != nil {
		_ = client.Close()
	}
}

type protocolFailure struct {
	status  connectors.TestStatus
	message string
}

func (failure protocolFailure) Error() string { return failure.message }

func newProtocolFailure(status connectors.TestStatus, message string) error {
	return protocolFailure{status: status, message: message}
}

func classifyProtocolError(phase string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return newProtocolFailure(connectors.TestFailedNetwork, fmt.Sprintf("%s timed out or was canceled", phase))
	}
	if errors.Is(err, ErrResponseTooLarge) {
		return newProtocolFailure(connectors.TestFailedNetwork, fmt.Sprintf("%s failed: protocol response exceeded %d bytes", phase, maxProtocolReadBytes))
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return newProtocolFailure(connectors.TestFailedNetwork, fmt.Sprintf("%s timed out", phase))
	}
	var tlsErr tls.RecordHeaderError
	if errors.As(err, &tlsErr) {
		return newProtocolFailure(connectors.TestFailedTLS, fmt.Sprintf("%s failed: invalid TLS response", phase))
	}
	var certificateError *tls.CertificateVerificationError
	var hostnameError x509.HostnameError
	var authorityError x509.UnknownAuthorityError
	var invalidCertificate x509.CertificateInvalidError
	if errors.As(err, &certificateError) || errors.As(err, &hostnameError) || errors.As(err, &authorityError) || errors.As(err, &invalidCertificate) || protocolTLSPhase(phase) {
		return newProtocolFailure(connectors.TestFailedTLS, fmt.Sprintf("%s failed TLS verification", phase))
	}
	if strings.Contains(strings.ToLower(phase), "auth") || strings.Contains(strings.ToLower(phase), "login") {
		return newProtocolFailure(connectors.TestFailedAuth, fmt.Sprintf("%s failed", phase))
	}
	return newProtocolFailure(connectors.TestFailedNetwork, fmt.Sprintf("%s failed", phase))
}

func protocolTLSPhase(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "tls handshake", "imap starttls", "smtp starttls":
		return true
	default:
		return false
	}
}

func (Connector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	ctx, cancel := context.WithTimeout(ctx, connectPhaseTimeout)
	defer cancel()
	config, err := targetConfigFrom(runtime.Target)
	if err != nil {
		return connectors.TestResult{Status: connectors.TestFailedConfig, Message: err.Error()}, nil
	}
	profile, err := profileConfigFrom(runtime.Profile)
	if err != nil {
		return connectors.TestResult{Status: connectors.TestFailedConfig, Message: err.Error()}, nil
	}
	type outcome struct {
		protocol string
		details  map[string]any
		err      error
	}
	protocols := 0
	enabledProtocols := make([]string, 0, 2)
	results := make(chan outcome, 2)
	if profile.IMAPEnabled {
		protocols++
		enabledProtocols = append(enabledProtocols, "imap")
		go func() {
			details := map[string]any{"enabled": true}
			secrets, err := loadIMAPSecrets(ctx, runtime)
			if err != nil {
				results <- outcome{protocol: "imap", details: details, err: newProtocolFailure(connectors.TestFailedAuth, err.Error())}
				return
			}
			imapClient, err := openIMAP(ctx, runtime, config, profile, secrets)
			if err == nil {
				capabilities, capabilityErr := imapClient.Capability()
				if capabilityErr == nil {
					details["move"] = capabilities["MOVE"]
					details["uidplus"] = capabilities["UIDPLUS"]
				}
				if noopErr := imapClient.Noop(); noopErr != nil {
					err = classifyProtocolError("IMAP NOOP", noopErr)
				}
				closeIMAP(imapClient)
			}
			results <- outcome{protocol: "imap", details: details, err: err}
		}()
	}
	if profile.SMTPAuthMode != "disabled" {
		protocols++
		enabledProtocols = append(enabledProtocols, "smtp")
		go func() {
			details := map[string]any{"enabled": true}
			secrets, err := loadSMTPSecrets(ctx, runtime, profile)
			if err != nil {
				results <- outcome{protocol: "smtp", details: details, err: newProtocolFailure(connectors.TestFailedAuth, err.Error())}
				return
			}
			smtpClient, err := openSMTP(ctx, runtime, config, profile, secrets)
			if err == nil {
				if noopErr := smtpClient.Noop(); noopErr != nil {
					err = classifyProtocolError("SMTP NOOP", noopErr)
				}
				closeSMTP(smtpClient)
			}
			results <- outcome{protocol: "smtp", details: details, err: err}
		}()
	}
	details := map[string]any{
		"imap": map[string]any{"enabled": false},
		"smtp": map[string]any{"enabled": false},
	}
	for _, protocol := range enabledProtocols {
		details[protocol] = map[string]any{"enabled": true}
	}
	failures := make([]string, 0, protocols)
	overall := connectors.TestOK
	received := map[string]bool{}
	for len(received) < protocols {
		var result outcome
		select {
		case result = <-results:
			received[result.protocol] = true
		case <-ctx.Done():
			for _, protocol := range enabledProtocols {
				if received[protocol] {
					continue
				}
				message := strings.ToUpper(protocol) + " connection test timed out"
				details[protocol] = map[string]any{
					"enabled": true,
					"ok":      false,
					"status":  connectors.TestFailedNetwork,
					"message": "connection test timed out",
				}
				failures = append(failures, message)
				received[protocol] = true
			}
			if testFailurePriority(connectors.TestFailedNetwork) > testFailurePriority(overall) {
				overall = connectors.TestFailedNetwork
			}
			continue
		}
		result.details["ok"] = result.err == nil
		if result.err != nil {
			status := protocolFailureStatus(result.err)
			result.details["status"] = status
			result.details["message"] = result.err.Error()
			failures = append(failures, strings.ToUpper(result.protocol)+" connection test failed: "+result.err.Error())
			if testFailurePriority(status) > testFailurePriority(overall) {
				overall = status
			}
		} else {
			result.details["status"] = connectors.TestOK
		}
		details[result.protocol] = result.details
	}
	if len(failures) > 0 {
		return connectors.TestResult{Status: overall, Message: strings.Join(failures, "; "), Details: details}, nil
	}
	return connectors.TestResult{Status: connectors.TestOK, Message: "Enabled mail protocols connected with verified TLS and authenticated successfully. No message was sent.", Details: details}, nil
}

func protocolFailureStatus(err error) connectors.TestStatus {
	var failure protocolFailure
	if errors.As(err, &failure) {
		return failure.status
	}
	return connectors.TestFailedNetwork
}

func testFailurePriority(status connectors.TestStatus) int {
	switch status {
	case connectors.TestFailedTLS:
		return 3
	case connectors.TestFailedAuth:
		return 2
	case connectors.TestFailedNetwork:
		return 1
	default:
		return 0
	}
}
