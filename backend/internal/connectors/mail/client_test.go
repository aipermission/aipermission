package mailconnector

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestConnectionClassifiesInvalidConfiguration(t *testing.T) {
	result, err := (Connector{}).TestConnection(t.Context(), connectors.RuntimeContext{
		Target:  connectors.TargetView{Config: map[string]any{"imap_host": "https://mail.example.com"}},
		Profile: connectors.CredentialProfileView{Public: validProfilePublic()},
	})
	if err != nil || result.Status != connectors.TestFailedConfig {
		t.Fatalf("invalid configuration result=%#v err=%v", result, err)
	}
}

func TestTLSConfigRequiresVerifiedTLS12WithServerName(t *testing.T) {
	config := tlsConfigFor("mail.example.com")
	if config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("minimum TLS version = %d, want TLS 1.2", config.MinVersion)
	}
	if config.ServerName != "mail.example.com" {
		t.Fatalf("server name = %q, want mail.example.com", config.ServerName)
	}
	if config.InsecureSkipVerify {
		t.Fatal("TLS certificate verification must remain enabled")
	}
}

func TestSMTPSTARTTLSRefreshesCapabilitiesBeforeAuthentication(t *testing.T) {
	certificate, roots := mailTestTLSCertificate(t)
	transport := newMailProtocolTransport(func(conn net.Conn) error {
		reader := bufio.NewReader(conn)
		if _, err := io.WriteString(conn, "220 mail.test ESMTP ready\r\n"); err != nil {
			return err
		}
		line, err := readProtocolLine(reader)
		if err != nil || !strings.HasPrefix(line, "EHLO ") {
			return fmt.Errorf("pre-TLS greeting = %q: %w", line, err)
		}
		if _, err := io.WriteString(conn, "250-mail.test\r\n250-STARTTLS\r\n250 AUTH PLAIN\r\n"); err != nil {
			return err
		}
		line, err = readProtocolLine(reader)
		if err != nil || line != "STARTTLS" {
			return fmt.Errorf("STARTTLS command = %q: %w", line, err)
		}
		if _, err := io.WriteString(conn, "220 begin TLS\r\n"); err != nil {
			return err
		}
		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return err
		}
		reader = bufio.NewReader(tlsConn)
		line, err = readProtocolLine(reader)
		if err != nil || !strings.HasPrefix(line, "EHLO ") {
			return fmt.Errorf("post-TLS greeting = %q: %w", line, err)
		}
		if _, err := io.WriteString(tlsConn, "250-mail.test\r\n250 AUTH PLAIN\r\n"); err != nil {
			return err
		}
		line, err = readProtocolLine(reader)
		if err != nil || !strings.HasPrefix(line, "AUTH PLAIN ") {
			return fmt.Errorf("post-TLS authentication = %q: %w", line, err)
		}
		if _, err = io.WriteString(tlsConn, "235 2.7.0 authenticated\r\n"); err != nil {
			return err
		}
		return waitForProtocolClose(reader)
	})
	runtime := mailProtocolRuntime(transport)
	client, err := openSMTPWithTLSConfig(t.Context(), runtime,
		targetConfig{ConnectionMode: "direct", SMTPHost: "mail.test", SMTPPort: 587, SMTPTLSMode: "starttls"},
		profileConfig{SMTPAuthMode: "separate"}, protocolSecrets{SMTPUsername: "user", SMTPPassword: "password"},
		func(host string) *tls.Config { return trustedMailTLSConfig(host, roots) },
	)
	if err != nil {
		t.Fatalf("open SMTP STARTTLS: %v", err)
	}
	_ = client.Close()
	if err := <-transport.done; err != nil {
		t.Fatalf("SMTP server: %v", err)
	}
}

func TestIMAPSTARTTLSRefreshesCapabilitiesBeforeAuthentication(t *testing.T) {
	certificate, roots := mailTestTLSCertificate(t)
	transport := newMailProtocolTransport(func(conn net.Conn) error {
		reader := bufio.NewReader(conn)
		if _, err := io.WriteString(conn, "* OK [CAPABILITY IMAP4rev1 STARTTLS] ready\r\n"); err != nil {
			return err
		}
		line, err := readProtocolLine(reader)
		if err != nil || !strings.Contains(line, " STARTTLS") {
			return fmt.Errorf("STARTTLS command = %q: %w", line, err)
		}
		tag := strings.Fields(line)[0]
		if _, err := io.WriteString(conn, tag+" OK begin TLS\r\n"); err != nil {
			return err
		}
		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return err
		}
		reader = bufio.NewReader(tlsConn)
		line, err = readProtocolLine(reader)
		if err != nil || !strings.Contains(line, " CAPABILITY") {
			return fmt.Errorf("post-TLS capability = %q: %w", line, err)
		}
		tag = strings.Fields(line)[0]
		if _, err := io.WriteString(tlsConn, "* CAPABILITY IMAP4rev1 AUTH=PLAIN\r\n"+tag+" OK capability complete\r\n"); err != nil {
			return err
		}
		line, err = readProtocolLine(reader)
		if err != nil || !strings.Contains(line, " LOGIN ") {
			return fmt.Errorf("post-TLS login = %q: %w", line, err)
		}
		tag = strings.Fields(line)[0]
		if _, err = io.WriteString(tlsConn, tag+" OK authenticated\r\n"); err != nil {
			return err
		}
		return waitForProtocolClose(reader)
	})
	runtime := mailProtocolRuntime(transport)
	client, err := openIMAPWithTLSConfig(t.Context(), runtime,
		targetConfig{ConnectionMode: "direct", IMAPHost: "mail.test", IMAPPort: 143, IMAPTLSMode: "starttls"},
		profileConfig{IMAPEnabled: true}, protocolSecrets{IMAPUsername: "user", IMAPPassword: "password"},
		func(host string) *tls.Config { return trustedMailTLSConfig(host, roots) },
	)
	if err != nil {
		t.Fatalf("open IMAP STARTTLS: %v", err)
	}
	_ = client.Terminate()
	if err := <-transport.done; err != nil {
		t.Fatalf("IMAP server: %v", err)
	}
}

func TestImplicitTLSRejectsUntrustedHostnameMismatchAndDowngrade(t *testing.T) {
	certificate, roots := mailTestTLSCertificate(t)
	tests := []struct {
		name      string
		serverTLS *tls.Config
		clientTLS tlsConfigFactory
	}{
		{
			name:      "untrusted certificate",
			serverTLS: &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12},
			clientTLS: tlsConfigFor,
		},
		{
			name:      "hostname mismatch",
			serverTLS: &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12},
			clientTLS: func(string) *tls.Config { return trustedMailTLSConfig("wrong.test", roots) },
		},
		{
			name:      "TLS downgrade",
			serverTLS: &tls.Config{Certificates: []tls.Certificate{certificate}, MaxVersion: tls.VersionTLS11},
			clientTLS: func(host string) *tls.Config { return trustedMailTLSConfig(host, roots) },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newMailProtocolTransport(func(conn net.Conn) error {
				return tls.Server(conn, test.serverTLS).Handshake()
			})
			_, err := openSMTPWithTLSConfig(t.Context(), mailProtocolRuntime(transport),
				targetConfig{ConnectionMode: "direct", SMTPHost: "mail.test", SMTPPort: 465, SMTPTLSMode: "implicit_tls"},
				profileConfig{SMTPAuthMode: "separate"}, protocolSecrets{SMTPUsername: "user", SMTPPassword: "password"}, test.clientTLS,
			)
			if err == nil {
				t.Fatal("expected verified TLS failure")
			}
			var failure protocolFailure
			if !errors.As(err, &failure) || failure.status != connectors.TestFailedTLS {
				t.Fatalf("TLS failure = %#v", err)
			}
			<-transport.done
		})
	}
}

type mailProtocolTransport struct {
	handler func(net.Conn) error
	done    chan error
}

func newMailProtocolTransport(handler func(net.Conn) error) *mailProtocolTransport {
	return &mailProtocolTransport{handler: handler, done: make(chan error, 1)}
}

func (*mailProtocolTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (transport *mailProtocolTransport) DialConnectorTCP(context.Context, connectors.NetworkDialRequest) (net.Conn, error) {
	clientConn, serverConn := net.Pipe()
	go func() {
		defer serverConn.Close()
		transport.done <- transport.handler(serverConn)
	}()
	return clientConn, nil
}

type mailProtocolCapabilities struct{ transport connectors.NetworkTransport }

func (capabilities mailProtocolCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return capabilities.transport
	}
	return nil
}

func mailProtocolRuntime(transport connectors.NetworkTransport) connectors.RuntimeContext {
	return connectors.RuntimeContext{
		Target:       connectors.TargetView{Ref: "mail:1:1"},
		Capabilities: mailProtocolCapabilities{transport: transport},
	}
}

func readProtocolLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	return strings.TrimSpace(line), err
}

func waitForProtocolClose(reader *bufio.Reader) error {
	_, err := reader.ReadString('\n')
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func mailTestTLSCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "mail.test"},
		DNSNames:     []string{"mail.test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: privateKey}
	roots := x509.NewCertPool()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	roots.AddCert(parsed)
	return certificate, roots
}

func trustedMailTLSConfig(host string, roots *x509.CertPool) *tls.Config {
	config := tlsConfigFor(host)
	config.RootCAs = roots
	return config
}

type fixedSMTPAuthCapabilities map[string]bool

func (capabilities fixedSMTPAuthCapabilities) SupportsAuth(mechanism string) bool {
	return capabilities[mechanism]
}

func TestSMTPPasswordAuthPrefersPlainAndSupportsLoginFallback(t *testing.T) {
	auth, err := smtpPasswordAuth(fixedSMTPAuthCapabilities{"PLAIN": true, "LOGIN": true}, "user", "password")
	if err != nil {
		t.Fatalf("plain auth: %v", err)
	}
	mechanism, _, err := auth.Start()
	if err != nil || mechanism != "PLAIN" {
		t.Fatalf("plain mechanism=%q err=%v", mechanism, err)
	}
	auth, err = smtpPasswordAuth(fixedSMTPAuthCapabilities{"LOGIN": true}, "user", "password")
	if err != nil {
		t.Fatalf("login auth: %v", err)
	}
	mechanism, _, err = auth.Start()
	if err != nil || mechanism != "LOGIN" {
		t.Fatalf("login mechanism=%q err=%v", mechanism, err)
	}
	if _, err := smtpPasswordAuth(fixedSMTPAuthCapabilities{}, "user", "password"); err == nil {
		t.Fatal("expected unsupported auth error")
	}
}

type memoryConn struct{ *bytes.Reader }

type zeroReadConn struct{ memoryConn }

func (zeroReadConn) Read([]byte) (int, error) { return 0, nil }

func (memoryConn) Write(buffer []byte) (int, error) { return len(buffer), nil }
func (memoryConn) Close() error                     { return nil }
func (memoryConn) LocalAddr() net.Addr              { return memoryAddr("local") }
func (memoryConn) RemoteAddr() net.Addr             { return memoryAddr("remote") }
func (memoryConn) SetDeadline(time.Time) error      { return nil }
func (memoryConn) SetReadDeadline(time.Time) error  { return nil }
func (memoryConn) SetWriteDeadline(time.Time) error { return nil }

type memoryAddr string

func (address memoryAddr) Network() string { return "memory" }
func (address memoryAddr) String() string  { return string(address) }

type trackedConn struct {
	memoryConn
	mu            sync.Mutex
	closed        bool
	deadline      time.Time
	readDeadline  time.Time
	writeDeadline time.Time
}

func (conn *trackedConn) Close() error {
	conn.mu.Lock()
	conn.closed = true
	conn.mu.Unlock()
	return nil
}

func (conn *trackedConn) SetDeadline(deadline time.Time) error {
	conn.mu.Lock()
	conn.deadline = deadline
	conn.mu.Unlock()
	return nil
}

func (conn *trackedConn) SetReadDeadline(deadline time.Time) error {
	conn.mu.Lock()
	conn.readDeadline = deadline
	conn.mu.Unlock()
	return nil
}

func (conn *trackedConn) SetWriteDeadline(deadline time.Time) error {
	conn.mu.Lock()
	conn.writeDeadline = deadline
	conn.mu.Unlock()
	return nil
}

func (conn *trackedConn) isClosed() bool {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.closed
}

func TestBoundedReadConnStopsOversizedProtocolResponses(t *testing.T) {
	source := bytes.Repeat([]byte("x"), maxProtocolReadBytes+1)
	conn := &boundedReadConn{Conn: memoryConn{Reader: bytes.NewReader(source)}}
	data, err := io.ReadAll(conn)
	if len(data) != maxProtocolReadBytes {
		t.Fatalf("read %d bytes, want %d", len(data), maxProtocolReadBytes)
	}
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("read error = %v, want ErrResponseTooLarge", err)
	}
}

func TestBoundedReadConnAcceptsAnExactBudgetResponse(t *testing.T) {
	source := bytes.Repeat([]byte("x"), maxProtocolReadBytes)
	conn := &boundedReadConn{Conn: memoryConn{Reader: bytes.NewReader(source)}}
	data, err := io.ReadAll(conn)
	if err != nil || len(data) != maxProtocolReadBytes {
		t.Fatalf("read %d bytes with %v, want exact budget", len(data), err)
	}
}

func TestBoundedReadConnRejectsAnEmptyProbeWithoutProgress(t *testing.T) {
	conn := &boundedReadConn{Conn: zeroReadConn{}, read: maxProtocolReadBytes}
	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("probe error = %v, want io.ErrNoProgress", err)
	}
}

func TestContextBoundConnectionClosesWhenActionIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	conn := &trackedConn{memoryConn: memoryConn{Reader: bytes.NewReader(nil)}}
	bound := bindConnToContext(ctx, conn)
	cancel()
	deadline := time.Now().Add(time.Second)
	for !conn.isClosed() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !conn.isClosed() {
		t.Fatal("connection remained open after action cancellation")
	}
	_ = bound.Close()
}

func TestConnectionDeadlineUsesTheTotalActionBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	conn := &trackedConn{memoryConn: memoryConn{Reader: bytes.NewReader(nil)}}
	if err := setConnDeadlineFromContext(conn, ctx, time.Minute); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	ctxDeadline, _ := ctx.Deadline()
	if conn.deadline.After(ctxDeadline) || ctxDeadline.Sub(conn.deadline) > time.Millisecond {
		t.Fatalf("connection deadline %s does not match context deadline %s", conn.deadline, ctxDeadline)
	}
}

func TestDeadlineCapPreventsProtocolClientsFromExtendingBootstrapBudget(t *testing.T) {
	base := &trackedConn{memoryConn: memoryConn{Reader: bytes.NewReader(nil)}}
	cap := time.Now().Add(time.Second)
	conn := newDeadlineCapConn(base, cap)
	if err := conn.SetDeadline(time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatalf("clear read deadline: %v", err)
	}
	if err := conn.SetWriteDeadline(time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("set write deadline: %v", err)
	}
	if !base.deadline.Equal(cap) || !base.readDeadline.Equal(cap) || !base.writeDeadline.Equal(cap) {
		t.Fatalf("deadlines escaped cap: deadline=%s read=%s write=%s cap=%s", base.deadline, base.readDeadline, base.writeDeadline, cap)
	}
	conn.release()
	if err := conn.SetDeadline(time.Time{}); err != nil {
		t.Fatalf("clear released deadline: %v", err)
	}
	if !base.deadline.IsZero() {
		t.Fatalf("released deadline = %s, want zero", base.deadline)
	}
}

func TestProtocolErrorsDoNotEchoServerSecrets(t *testing.T) {
	err := classifyProtocolError("IMAP NOOP", errors.New("NO password=super-secret Bearer abcdefghijklmnopqrstuvwxyz"))
	if strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "Bearer") {
		t.Fatalf("classified error leaked server text: %q", err)
	}
}

func TestProtocolErrorClassificationLimitsTLSStatusToTLSPhases(t *testing.T) {
	tlsFailure := classifyProtocolError("IMAP STARTTLS", errors.New("unavailable"))
	if failure, ok := tlsFailure.(protocolFailure); !ok || failure.status != connectors.TestFailedTLS {
		t.Fatalf("STARTTLS failure = %#v", tlsFailure)
	}
	networkFailure := classifyProtocolError("IMAP post-TLS capability", errors.New("connection lost"))
	if failure, ok := networkFailure.(protocolFailure); !ok || failure.status != connectors.TestFailedNetwork {
		t.Fatalf("post-TLS capability failure = %#v", networkFailure)
	}
}

type recordingSecrets struct {
	values map[string]string
	calls  []string
}

func (secrets *recordingSecrets) GetSecret(_ context.Context, name string) (string, error) {
	secrets.calls = append(secrets.calls, name)
	return secrets.values[name], nil
}

func TestProtocolSecretLoadersReadOnlyRequiredCredentials(t *testing.T) {
	imap := &recordingSecrets{values: map[string]string{"imap_username": "reader", "imap_password": "imap-secret"}}
	loadedIMAP, err := loadIMAPSecrets(t.Context(), connectors.RuntimeContext{Secrets: imap})
	if err != nil || loadedIMAP.IMAPUsername != "reader" || strings.Join(imap.calls, ",") != "imap_username,imap_password" {
		t.Fatalf("IMAP secrets=%#v calls=%v err=%v", loadedIMAP, imap.calls, err)
	}

	smtp := &recordingSecrets{values: map[string]string{"smtp_username": "sender", "smtp_password": "smtp-secret"}}
	loadedSMTP, err := loadSMTPSecrets(t.Context(), connectors.RuntimeContext{Secrets: smtp}, profileConfig{SMTPAuthMode: "separate"})
	if err != nil || loadedSMTP.SMTPUsername != "sender" || strings.Join(smtp.calls, ",") != "smtp_username,smtp_password" {
		t.Fatalf("SMTP secrets=%#v calls=%v err=%v", loadedSMTP, smtp.calls, err)
	}

	reused := &recordingSecrets{values: map[string]string{"imap_username": "shared", "imap_password": "shared-secret"}}
	loadedReused, err := loadSMTPSecrets(t.Context(), connectors.RuntimeContext{Secrets: reused}, profileConfig{SMTPAuthMode: "reuse_imap"})
	if err != nil || loadedReused.SMTPUsername != "shared" || strings.Join(reused.calls, ",") != "imap_username,imap_password" {
		t.Fatalf("reused SMTP secrets=%#v calls=%v err=%v", loadedReused, reused.calls, err)
	}
}
