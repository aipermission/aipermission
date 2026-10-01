package kubernetesconnector

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// --help validates the real CLI grammar without contacting any Kubernetes API.
func TestConnectionActualKubectlGrammar(t *testing.T) {
	for _, mode := range []string{"all", "selected"} {
		t.Run(mode, func(t *testing.T) {
			target, transport := ownedKubectl(t)
			transport.help = true
			target.Config["context"] = "owned context"
			result, err := New().TestConnection(context.Background(), connectors.RuntimeContext{
				Target: target, Profile: kubeProfile(mode), Capabilities: fakeCapabilities{transport: transport},
			})
			if err != nil || result.Status != connectors.TestOK {
				t.Fatalf("actual kubectl rejected probe: result=%#v err=%v", result, err)
			}
		})
	}
}

type cliProbeTransport struct {
	env      []string
	help     bool
	requests []connectors.CommandRunRequest
}

func (*cliProbeTransport) ConnectorRuntimeCapability() string {
	return connectors.CommandTransportCapabilityName
}

func (transport *cliProbeTransport) RunConnectorCommand(ctx context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	transport.requests = append(transport.requests, request)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutSeconds)*time.Second)
	defer cancel()
	command := "exec " + request.Command
	if transport.help {
		command += " --help"
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Env = transport.env
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	result := connectors.CommandRunResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		result.ExitCode = exit.ExitCode()
		return result, nil
	}
	return result, err
}

func ownedKubectl(t *testing.T) (connectors.TargetView, *cliProbeTransport) {
	t.Helper()
	binary, err := exec.LookPath("kubectl")
	if err != nil {
		t.Fatalf("Linux backend tests require kubectl on PATH: %v", err)
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	target := kubeTarget()
	target.Config["kubectl_command"] = binary
	return target, &cliProbeTransport{env: []string{
		"HOME=" + directory,
		"KUBECONFIG=" + filepath.Join(directory, "kubeconfig.json"),
		"PATH=" + os.Getenv("PATH"),
		"LANG=C",
	}}
}
