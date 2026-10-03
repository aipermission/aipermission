package daemonconformance_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"golang.org/x/crypto/ssh"
)

type guestTransport struct{ target execution.Target }

func (guestTransport) ConnectorRuntimeCapability() string {
	return connectors.CommandTransportCapabilityName
}
func (transport guestTransport) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.CommandTransportCapabilityName {
		return transport
	}
	return nil
}

func (transport guestTransport) RunConnectorCommand(ctx context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	if request.SourceTargetRef != "docker:1:1" || request.TransportTargetRef != "ssh:2:2" || request.Mode != "over_ssh" || request.TimeoutSeconds < 1 || request.TimeoutSeconds > 60 {
		return connectors.CommandRunResult{}, fmt.Errorf("guest refused unexpected command transport")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutSeconds)*time.Second)
	defer cancel()
	result, err := execution.RunCommand(ctx, transport.target, request.Command)
	return connectors.CommandRunResult{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode, DurationMS: result.DurationMS, DispatchStarted: result.DispatchStarted}, err
}

func guestSSHTransport(t *testing.T) guestTransport {
	t.Helper()
	private, err := os.ReadFile("/run/ssh_client")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile("/run/ssh_host.pub")
	if err != nil {
		t.Fatal(err)
	}
	public, _, _, _, err := ssh.ParseAuthorizedKey(encoded)
	if err != nil {
		t.Fatal(err)
	}
	target := execution.Target{Host: "127.0.0.1", Port: 22, Username: "aipermission", PrivateKey: string(private), KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts")}
	identity := execution.NewUnknownHostKeyError("127.0.0.1:22", public)
	if err := execution.TrustHostKey(target.KnownHostsPath, "127.0.0.1:22", identity.PublicKey); err != nil {
		t.Fatal(err)
	}
	return guestTransport{target: target}
}
