package mailconnector

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	stdmail "net/mail"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectorcredentials"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

// A server may reflect the reusable AUTH payload in an otherwise valid response.
func TestSMTPPlainWireCredentialIsRedacted(t *testing.T) {
	for _, tc := range []struct{ mode, phase string }{{"basic", "accepted"}, {"off", "accepted"}, {"basic", "auth-refused"}, {"off", "auth-refused"}} {
		t.Run(tc.mode+"-"+tc.phase, func(t *testing.T) {
			const username = "review-user"
			const password = "review-password-8uX4qz"
			certificate, roots := mailTestTLSCertificate(t)
			observed := make(chan string, 1)
			transport := newMailProtocolTransport(func(conn net.Conn) error {
				tlsConn, err := negotiateSMTPTestSTARTTLS(conn, certificate)
				if err != nil {
					return err
				}
				reader := bufio.NewReader(tlsConn)
				line, err := readProtocolLine(reader)
				if err != nil || !strings.HasPrefix(line, "EHLO ") {
					return fmt.Errorf("EHLO: %q %v", line, err)
				}
				if _, err = io.WriteString(tlsConn, "250-mail.test\r\n250 AUTH PLAIN\r\n"); err != nil {
					return err
				}
				line, err = readProtocolLine(reader)
				if err != nil || !strings.HasPrefix(line, "AUTH PLAIN ") {
					return fmt.Errorf("AUTH: %q %v", line, err)
				}
				wire := strings.TrimPrefix(line, "AUTH PLAIN ")
				observed <- wire
				if tc.phase == "auth-refused" {
					if _, err = io.WriteString(tlsConn, "535 5.7.8 denied proof="+wire+"\r\n"); err != nil {
						return err
					}
					return waitForProtocolClose(reader)
				}
				if _, err = io.WriteString(tlsConn, "235 2.7.0 authenticated\r\n"); err != nil {
					return err
				}
				for _, command := range []struct{ prefix, reply string }{
					{"MAIL FROM:", "250 2.1.0 sender ok\r\n"},
					{"RCPT TO:", "250 2.1.5 recipient ok\r\n"},
					{"DATA", "354 send message\r\n"},
				} {
					line, err = readProtocolLine(reader)
					if err != nil || !strings.HasPrefix(line, command.prefix) {
						return fmt.Errorf("want %s, got %q: %v", command.prefix, line, err)
					}
					if _, err = io.WriteString(tlsConn, command.reply); err != nil {
						return err
					}
				}
				for {
					line, err = readProtocolLine(reader)
					if err != nil {
						return err
					}
					if line == "." {
						break
					}
				}
				if _, err = io.WriteString(tlsConn, "250 2.0.0 accepted proof="+wire+"\r\n"); err != nil {
					return err
				}
				return waitForProtocolClose(reader)
			})
			runtime := mailProtocolRuntime(transport)
			secretValues := map[string]any{"smtp_username": username, "smtp_password": password}
			boundary := actionresult.NewCredentialBoundary(secretValues)
			runtime.Secrets = connectorcredentials.Secrets(secretValues, boundary)
			profile := profileConfig{SMTPAuthMode: "separate"}
			secrets, err := loadSMTPSecrets(t.Context(), runtime, profile)
			if err != nil {
				t.Fatal(err)
			}
			client, err := openSMTPWithTLSConfig(t.Context(), runtime,
				targetConfig{ConnectionMode: "direct", SMTPHost: "mail.test", SMTPPort: 587, SMTPTLSMode: "starttls"},
				profile, secrets, func(host string) *tls.Config { return trustedMailTLSConfig(host, roots) })
			var result connectors.ActionResult
			if tc.phase == "auth-refused" {
				if err == nil {
					t.Fatal("refused authentication succeeded")
				}
				result = connectors.ActionResult{Status: connectors.ResultFailed, DisplayText: err.Error(), Output: map[string]any{"error": err.Error()}}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				result = submitSMTPMessage(smtpClientAdapter{client: client}, "sender@example.com", outboundMessage{To: []*stdmail.Address{{Address: "recipient@example.com"}}}, []byte("Subject: Review fixture\r\n\r\nSynthetic body\r\n"), "<review@example.com>")
				_ = client.Close()
			}
			if err := <-transport.done; err != nil {
				t.Fatalf("fixture server: %v", err)
			}
			if tc.phase == "accepted" && result.Status != connectors.ResultCompleted {
				t.Fatalf("SMTP did not complete: %#v", result)
			}
			wire := <-observed
			decoded, err := base64.StdEncoding.DecodeString(wire)
			if err != nil || string(decoded) != "\x00"+username+"\x00"+password {
				t.Fatal("fixture did not observe exact reusable PLAIN credential")
			}
			optional := func(_ context.Context, value string) string {
				if tc.mode == "basic" {
					return securitypolicy.RedactBasic(value)
				}
				return value
			}
			redactor, err := actionresult.NewRedactor(optional, optional, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			projected, err := redactor.ResultWithCredentialBoundary(t.Context(), result, boundary)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(projected)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), wire) {
				t.Fatalf("SMTP PLAIN credential survived result projection (mode=%s)", tc.mode)
			}
		})
	}
}
