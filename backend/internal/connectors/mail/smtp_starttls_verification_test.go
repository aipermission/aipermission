package mailconnector

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func negotiateSMTPTestSTARTTLS(conn net.Conn, certificate tls.Certificate) (*tls.Conn, error) {
	reader := bufio.NewReader(conn)
	for _, step := range []struct{ response, command string }{
		{"220 mail.test ESMTP ready\r\n", "EHLO "},
		{"250-mail.test\r\n250-STARTTLS\r\n250 AUTH PLAIN\r\n", "STARTTLS"},
	} {
		if _, err := io.WriteString(conn, step.response); err != nil {
			return nil, err
		}
		line, err := readProtocolLine(reader)
		matches := line == step.command || strings.HasSuffix(step.command, " ") && strings.HasPrefix(line, step.command)
		if err != nil || !matches {
			return nil, fmt.Errorf("SMTP pre-TLS command %q: %q / %v", step.command, line, err)
		}
	}
	if _, err := io.WriteString(conn, "220 begin TLS\r\n"); err != nil {
		return nil, err
	}
	secured := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if err := secured.Handshake(); err != nil {
		return nil, err
	}
	return secured, nil
}

func TestSMTPSTARTTLSReportsTLSIdentityFailureBeforeAuthDiscovery(t *testing.T) {
	certificate, roots := mailTestTLSCertificate(t)
	for _, failure := range []string{"hostname", "authority"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			transport := newMailProtocolTransport(func(conn net.Conn) error {
				_, err := negotiateSMTPTestSTARTTLS(conn, certificate)
				return err
			})
			factory := func(host string) *tls.Config {
				config := trustedMailTLSConfig(host, roots)
				if failure == "hostname" {
					config.ServerName = "different.test"
				} else {
					config.RootCAs = x509.NewCertPool()
				}
				return config
			}
			client, err := openSMTPWithTLSConfig(ctx, mailProtocolRuntime(transport),
				targetConfig{ConnectionMode: "direct", SMTPHost: "mail.test", SMTPPort: 587, SMTPTLSMode: "starttls"},
				profileConfig{SMTPAuthMode: "separate"}, protocolSecrets{SMTPUsername: "fixture-user", SMTPPassword: "fixture-secret"}, factory)
			if client != nil {
				_ = client.Close()
			}
			var classified protocolFailure
			if !errors.As(err, &classified) || classified.status != connectors.TestFailedTLS || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatalf("SMTP %s verification failure was masked as authentication discovery: %v", failure, err)
			}
			select {
			case serverErr := <-transport.done:
				if serverErr == nil {
					t.Fatal("SMTP server accepted an invalid TLS identity")
				}
			case <-ctx.Done():
				t.Fatal("SMTP TLS verification did not release the server connection")
			}
		})
	}
}

func TestSMTPPostTLSGreetingFailurePreventsAuthAndClosesConnection(t *testing.T) {
	certificate, roots := mailTestTLSCertificate(t)
	for _, failure := range []string{"rejected", "stalled"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			transport := newMailProtocolTransport(func(conn net.Conn) error {
				secured, err := negotiateSMTPTestSTARTTLS(conn, certificate)
				if err != nil {
					return err
				}
				reader := bufio.NewReader(secured)
				line, err := readProtocolLine(reader)
				if err != nil || !strings.HasPrefix(line, "EHLO ") {
					return fmt.Errorf("post-TLS EHLO was not observed: %q / %v", line, err)
				}
				if failure == "rejected" {
					if _, err := io.WriteString(secured, "550 fixture-private-server-text\r\n"); err != nil {
						return err
					}
				}
				line, err = readProtocolLine(reader)
				if err == nil {
					return fmt.Errorf("unexpected command after failed post-TLS greeting: %q", line)
				}
				if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					return err
				}
				return nil
			})
			client, err := openSMTPWithTLSConfig(ctx, mailProtocolRuntime(transport),
				targetConfig{ConnectionMode: "direct", SMTPHost: "mail.test", SMTPPort: 587, SMTPTLSMode: "starttls"},
				profileConfig{SMTPAuthMode: "separate"}, protocolSecrets{SMTPUsername: "fixture-user", SMTPPassword: "fixture-secret"},
				func(host string) *tls.Config { return trustedMailTLSConfig(host, roots) })
			if client != nil {
				_ = client.Close()
			}
			var classified protocolFailure
			if !errors.As(err, &classified) || classified.status != connectors.TestFailedNetwork || strings.Contains(err.Error(), "fixture-private") {
				t.Fatalf("post-TLS greeting failure was misclassified or exposed server data: %v", err)
			}
			select {
			case serverErr := <-transport.done:
				if serverErr != nil {
					t.Fatal(serverErr)
				}
			case <-time.After(time.Second):
				t.Fatal("failed post-TLS greeting left a server connection open")
			}
		})
	}
}
