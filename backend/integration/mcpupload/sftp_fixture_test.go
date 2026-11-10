package mcpupload

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func startUploadSFTPFixture(t *testing.T) (*net.TCPAddr, ssh.PublicKey) {
	t.Helper()
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
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			serveUploadSFTPFixture(conn, config)
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(11 * time.Second):
			t.Error("SFTP test fixture did not close")
		}
	})
	return listener.Addr().(*net.TCPAddr), signer.PublicKey()
}

func serveUploadSFTPFixture(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()
	server, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer server.Close()
	go ssh.DiscardRequests(requests)
	for next := range channels {
		if next.ChannelType() != "session" {
			_ = next.Reject(ssh.UnknownChannelType, "expected session")
			continue
		}
		channel, requests, err := next.Accept()
		if err != nil {
			return
		}
		for request := range requests {
			if request.Type != "subsystem" || string(request.Payload) != "\x00\x00\x00\x04sftp" {
				_ = request.Reply(false, nil)
				continue
			}
			_ = request.Reply(true, nil)
			protocol, err := sftp.NewServer(channel)
			if err == nil {
				_ = protocol.Serve()
				_ = protocol.Close()
			}
			break
		}
		_ = channel.Close()
	}
}
