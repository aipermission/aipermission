package conformance_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
)

func protocolSSHTarget(t *testing.T) execution.Target {
	t.Helper()
	requireProtocolFixture(t)
	key, err := os.ReadFile("/fixture-material/ssh_client")
	if err != nil {
		t.Fatal(err)
	}
	return execution.Target{Host: protocolFixtureHost, Port: 22, Username: "aipermission", PrivateKey: string(key), KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts")}
}

type scopedSSHCommands struct {
	target execution.Target
	calls  int
}

func (transport *scopedSSHCommands) ConnectorRuntimeCapability() string {
	return connectors.CommandTransportCapabilityName
}

func (transport *scopedSSHCommands) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.CommandTransportCapabilityName {
		return transport
	}
	return nil
}

func (transport *scopedSSHCommands) RunConnectorCommand(ctx context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	if request.SourceTargetRef != "kubernetes:9:9" || request.Mode != "over_ssh" || request.TransportTargetRef != "ssh:8:8" || request.TimeoutSeconds < 1 || request.TimeoutSeconds > 60 {
		return connectors.CommandRunResult{}, fmt.Errorf("command fixture refused an unexpected transport identity")
	}
	transport.calls++
	ctx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutSeconds)*time.Second)
	defer cancel()
	result, err := execution.RunCommand(ctx, transport.target, request.Command)
	return connectors.CommandRunResult{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode, DurationMS: result.DurationMS, DispatchStarted: result.DispatchStarted}, err
}
