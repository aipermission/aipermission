package management

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type cleanupSSHServer struct {
	listener            net.Listener
	homes               map[string]string
	host                ssh.Signer
	ctx                 context.Context
	cancel              context.CancelFunc
	done                chan struct{}
	authAttempts        atomic.Int64
	commands            atomic.Int64
	loseReply           atomic.Bool
	blockReply          atomic.Bool
	absentBeforeCommand atomic.Bool
	executed            chan struct{}
	executedOnce        sync.Once
	mu                  sync.Mutex
	active              net.Conn
}

func startCleanupSSHServer(t *testing.T, fixture *cleanupFixture, users ...string) *cleanupSSHServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := &cleanupSSHServer{listener: listener, host: signer, homes: map[string]string{}, ctx: ctx, cancel: cancel, done: make(chan struct{}), executed: make(chan struct{})}
	root := t.TempDir()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		server.mu.Lock()
		if server.active != nil {
			_ = server.active.Close()
		}
		server.mu.Unlock()
		select {
		case <-server.done:
		case <-time.After(5 * time.Second):
			t.Error("SSH cleanup fixture failed to stop")
		}
	})
	for _, user := range users {
		home := filepath.Join(root, user)
		if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
			cancel()
			close(server.done)
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".ssh", "authorized_keys"), []byte(fixture.key.PublicKey+"\n"), 0o600); err != nil {
			cancel()
			close(server.done)
			t.Fatal(err)
		}
		server.homes[user] = home
	}
	host, text, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(text)
	fixture.target.Config["host"], fixture.target.Config["port"] = host, port
	if err := os.WriteFile(fixture.gateway.path, []byte(knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey())+"\n"), 0o600); err != nil {
		cancel()
		close(server.done)
		t.Fatal(err)
	}
	go server.serve()
	return server
}

func (server *cleanupSSHServer) serve() {
	defer close(server.done)
	for {
		connection, err := server.listener.Accept()
		if err != nil {
			return
		}
		server.mu.Lock()
		if server.ctx.Err() != nil {
			server.mu.Unlock()
			_ = connection.Close()
			return
		}
		server.active = connection
		server.mu.Unlock()
		server.handle(connection)
		server.mu.Lock()
		server.active = nil
		server.mu.Unlock()
	}
}

func (server *cleanupSSHServer) handle(connection net.Conn) {
	defer connection.Close()
	config := &ssh.ServerConfig{PublicKeyCallback: func(metadata ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		server.authAttempts.Add(1)
		home, exists := server.homes[metadata.User()]
		if !exists {
			return nil, errors.New("unknown fixture user")
		}
		data, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
		if err != nil || !strings.Contains(string(data), publicKeyBlob(string(ssh.MarshalAuthorizedKey(key)))) {
			return nil, errors.New("fixture key is revoked")
		}
		return nil, nil
	}}
	config.AddHostKey(server.host)
	peer, channels, requests, err := ssh.NewServerConn(connection, config)
	if err != nil {
		return
	}
	discarded := make(chan struct{})
	go func() { defer close(discarded); ssh.DiscardRequests(requests) }()
	defer func() { _ = peer.Close(); <-discarded }()
	channelRequest, ok := <-channels
	if !ok {
		return
	}
	channel, channelRequests, err := channelRequest.Accept()
	if err != nil {
		return
	}
	defer channel.Close()
	for request := range channelRequests {
		if request.Type != "exec" {
			_ = request.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		if ssh.Unmarshal(request.Payload, &payload) != nil {
			_ = request.Reply(false, nil)
			return
		}
		_ = request.Reply(true, nil)
		server.commands.Add(1)
		if server.absentBeforeCommand.Load() {
			if err := os.Remove(filepath.Join(server.homes[peer.User()], ".ssh", "authorized_keys")); err != nil {
				return
			}
		}
		ctx, cancel := context.WithTimeout(server.ctx, 5*time.Second)
		command := exec.CommandContext(ctx, "sh", "-c", payload.Command)
		command.Env = append(os.Environ(), "HOME="+server.homes[peer.User()])
		command.Stdout, command.Stderr = channel, channel.Stderr()
		err := command.Run()
		cancel()
		server.executedOnce.Do(func() { close(server.executed) })
		if server.blockReply.Load() {
			<-server.ctx.Done()
			return
		}
		if server.loseReply.Load() {
			return
		}
		status := uint32(0)
		if err != nil {
			status = 1
		}
		_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
		return
	}
}
