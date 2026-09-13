package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter/management"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type cancellationStage string

const (
	stageSession cancellationStage = "session"
	stagePTY     cancellationStage = "pty"
	stageShell   cancellationStage = "shell"
)

type testPeerIdentityGateway struct{ path string }

func (g testPeerIdentityGateway) ConnectorTrustStorePath() string { return g.path }

func TestLiveConsoleSetupStopsAtEveryCancellationStage(t *testing.T) {
	for _, stage := range []cancellationStage{stageSession, stagePTY, stageShell} {
		t.Run(string(stage), func(t *testing.T) {
			listener, target, gateway, privateKey, reached, serverDone := startCancellationSSHServer(t, stage)
			defer listener.Close()

			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			go func() {
				_, err := openLiveConsoleWithMaterial(ctx, gateway, target, privateKey, 24, 80, LiveConsoleOptions{})
				result <- err
			}()

			select {
			case <-reached:
			case <-time.After(2 * time.Second):
				t.Fatalf("SSH fixture did not reach %s setup", stage)
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("opening error = %v, want context canceled", err)
				}
			case <-time.After(time.Second):
				t.Fatalf("live console remained blocked in %s setup", stage)
			}
			select {
			case <-serverDone:
			case <-time.After(time.Second):
				t.Fatalf("SSH socket remained open after %s cancellation", stage)
			}
		})
	}
}

func startCancellationSSHServer(t *testing.T, stage cancellationStage) (net.Listener, management.TargetMaterial, testPeerIdentityGateway, string, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	hostSigner, _ := testSSHSigner(t)
	_, clientPrivate := testSSHSigner(t)
	privateBlock, err := ssh.MarshalPrivateKey(clientPrivate, "aipermission-test")
	if err != nil {
		t.Fatalf("marshal client key: %v", err)
	}
	knownHostsPath := t.TempDir() + "/known_hosts"
	line := knownhosts.Line([]string{listener.Addr().String()}, hostSigner.PublicKey()) + "\n"
	if err := os.WriteFile(knownHostsPath, []byte(line), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	reached := make(chan struct{})
	done := make(chan struct{})
	go serveCancellationStage(listener, hostSigner, stage, reached, done)
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return listener, management.TargetMaterial{Host: host, Port: port, Username: "test"}, testPeerIdentityGateway{path: knownHostsPath}, string(pem.EncodeToMemory(privateBlock)), reached, done
}

func testSSHSigner(t *testing.T) (ssh.Signer, ed25519.PrivateKey) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	return signer, privateKey
}

func serveCancellationStage(listener net.Listener, signer ssh.Signer, stage cancellationStage, reached chan<- struct{}, done chan<- struct{}) {
	defer close(done)
	connection, err := listener.Accept()
	if err != nil {
		return
	}
	defer connection.Close()
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	server, channels, requests, err := ssh.NewServerConn(connection, config)
	if err != nil {
		return
	}
	defer server.Close()
	go ssh.DiscardRequests(requests)
	channelRequest, ok := <-channels
	if !ok {
		return
	}
	if stage == stageSession {
		close(reached)
		_ = server.Wait()
		return
	}
	channel, channelRequests, err := channelRequest.Accept()
	if err != nil {
		return
	}
	defer channel.Close()
	for request := range channelRequests {
		switch request.Type {
		case "pty-req":
			if stage == stagePTY {
				close(reached)
				_ = server.Wait()
				return
			}
			_ = request.Reply(true, nil)
		case "shell":
			close(reached)
			_ = server.Wait()
			return
		default:
			_ = request.Reply(false, nil)
		}
	}
}
