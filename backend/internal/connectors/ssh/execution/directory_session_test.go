package execution

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestDirectorySessionDrainsStderrAndClosesPeer(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	root := t.TempDir()
	stderrDone := make(chan struct{})
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		server, channels, requests, err := ssh.NewServerConn(connection, config)
		if err != nil {
			return
		}
		defer server.Close()
		go ssh.DiscardRequests(requests)
		for next := range channels {
			channel, requests, err := next.Accept()
			if err != nil {
				return
			}
			for request := range requests {
				if request.Type != "subsystem" {
					_ = request.Reply(false, nil)
					continue
				}
				_ = request.Reply(true, nil)
				go func() {
					_, _ = io.Copy(channel.Stderr(), strings.NewReader(strings.Repeat("x", 3<<20)))
					close(stderrDone)
				}()
				protocol, err := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(root))
				if err == nil {
					_ = protocol.Serve()
					_ = protocol.Close()
				}
				_ = channel.Close()
				break
			}
		}
	}()
	connection, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{User: "fixture", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var budget *directoryResponseBudget
	client, err := setupWithContext(ctx, connection, func() (*sftp.Client, error) {
		var startErr error
		var client *sftp.Client
		client, budget, startErr = newDirectorySFTPClient(connection)
		return client, startErr
	})
	if err != nil {
		t.Fatal(err)
	}
	stop := closeOnContext(ctx, connection)
	defer stop()
	if _, err := listRemoteDirectoryWithClient(client, "~"); err != nil || budget.failure() != nil {
		t.Fatalf("browse: %v", err)
	}
	select {
	case <-stderrDone:
	case <-ctx.Done():
		t.Fatal("stderr blocked directory browsing")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("client cleanup exceeded deadline")
	}
}
