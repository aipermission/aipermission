package apiadapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	dockerconnector "github.com/aipermission/aipermission/backend/internal/connectors/docker"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestDockerExecShellCommandUsesValidatedExecutable(t *testing.T) {
	target := connectors.TargetView{Config: map[string]any{"docker_command": "/usr/local/bin/docker-wrapper"}}
	command, err := dockerconnector.DockerShellCommand(target)
	if err != nil {
		t.Fatalf("resolve Docker command: %v", err)
	}
	got := dockerExecShellCommand(command, "api")
	if !strings.Contains(got, "'/usr/local/bin/docker-wrapper' exec -it -- 'api'") || !strings.Contains(got, "Server.Version") {
		t.Fatalf("unexpected live-console command: %q", got)
	}
}

func TestDockerExecShellCommandRejectsUnsafeExecutableBeforeConstruction(t *testing.T) {
	for _, unsafe := range []string{"docker --debug", "docker; id", "$(id)", "eval", "true", "podman-docker"} {
		if _, err := dockerconnector.DockerCommand(connectors.TargetView{Config: map[string]any{"docker_command": unsafe}}); err == nil {
			t.Fatalf("expected %q to be rejected", unsafe)
		}
	}
}

func TestDockerExecShellCommandQuotesContainerAndRejectsInvalidRef(t *testing.T) {
	if dockerconnector.ValidContainerRef("; id >/tmp/pwn; #") {
		t.Fatal("expected shell payload to be rejected as a container ref")
	}
	if !dockerconnector.ValidContainerRef("api-1.2") {
		t.Fatal("expected normal Docker container name to be accepted")
	}
}

func TestOpenLiveConsoleResolvesActualContainerBeforeScopeCheck(t *testing.T) {
	target := connectors.TargetView{
		ID: 1, Ref: "docker:1:10", ConnectorKind: dockerconnector.Kind,
		Config: map[string]any{"transport_target_ref": "ssh:2:20", "connection_mode": "over_ssh"},
	}
	profile := connectors.CredentialProfileView{
		ID: 10, TargetID: 1, ConnectorKind: dockerconnector.Kind,
		Public: map[string]any{"scope_mode": "selected", "allowed_containers": "api"},
	}
	runtime := fakeLiveConsoleRuntime{target: target, profile: profile}
	gateway := &fakeLiveConsoleGateway{inventory: strings.Join([]string{
		`{"ID":"aaaaaaaaaaaa1111111111111111111111111111111111111111111111111111","Names":"api-other","Image":"api","State":"running","Status":"Up"}`,
		`{"ID":"bbbbbbbbbbbb2222222222222222222222222222222222222222222222222222","Names":"api","Image":"api","State":"running","Status":"Up"}`,
	}, "\n")}

	_, err := adapter{}.OpenLiveConsole(t.Context(), gateway, runtime, connectorapi.LiveConsoleOpenRequest{
		RuntimeID: 7, Params: map[string]any{"container": "api-other"},
	})
	if !errors.Is(err, dockerconnector.ErrScopeDenied) || gateway.openCalls != 0 {
		t.Fatalf("out-of-scope prefix opened console: calls=%d err=%v", gateway.openCalls, err)
	}

	_, err = adapter{}.OpenLiveConsole(t.Context(), gateway, runtime, connectorapi.LiveConsoleOpenRequest{
		RuntimeID: 7, Params: map[string]any{"container": "api"},
	})
	if err != nil || gateway.openCalls != 1 {
		t.Fatalf("exact scoped container did not open: calls=%d err=%v", gateway.openCalls, err)
	}
	command := gateway.lastParams["force_shell_command"].(string)
	if !strings.Contains(command, "'bbbbbbbbbbbb2222222222222222222222222222222222222222222222222222'") || strings.Contains(command, " -- 'api' ") {
		t.Fatalf("live console did not use the verified full container ID: %q", command)
	}
}

type fakeLiveConsoleRuntime struct {
	target  connectors.TargetView
	profile connectors.CredentialProfileView
}

func (runtime fakeLiveConsoleRuntime) TargetProfileByRuntimeID(context.Context, int64) (connectors.TargetView, connectors.CredentialProfileView, connectorapi.RuntimeSurface, error) {
	return runtime.target, runtime.profile, connectorapi.RuntimeSurface{
		ConnectorKind: dockerconnector.Kind, CapabilityKind: "live_console",
	}, nil
}
func (fakeLiveConsoleRuntime) ResolveConnectorActionTarget(context.Context, string) (connectors.TargetView, connectors.CredentialProfileView, error) {
	return connectors.TargetView{}, connectors.CredentialProfileView{}, errors.New("not implemented")
}
func (fakeLiveConsoleRuntime) EnsureRuntimeSurface(context.Context, connectorapi.EnsureRuntimeSurfaceInput) (connectorapi.RuntimeSurface, error) {
	return connectorapi.RuntimeSurface{}, errors.New("not implemented")
}
func (fakeLiveConsoleRuntime) ListRuntimeSurfacesForProfile(context.Context, int64, int64, string) ([]connectorapi.RuntimeSurface, error) {
	return nil, errors.New("not implemented")
}
func (fakeLiveConsoleRuntime) ListCredentialProfiles(context.Context, int64) ([]connectors.CredentialProfileView, error) {
	return nil, errors.New("not implemented")
}
func (fakeLiveConsoleRuntime) CredentialResources(string) connectorapi.CredentialResourceStore {
	return nil
}

type fakeLiveConsoleGateway struct {
	inventory  string
	openCalls  int
	lastParams map[string]any
}

func (*fakeLiveConsoleGateway) ConnectorTrustStorePath() string { return "" }
func (gateway *fakeLiveConsoleGateway) ConnectorRunCommand(_ context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	if !strings.Contains(request.Command, " ps -a --no-trunc ") {
		return connectors.CommandRunResult{}, errors.New("unexpected command")
	}
	return connectors.CommandRunResult{Stdout: gateway.inventory}, nil
}
func (gateway *fakeLiveConsoleGateway) ConnectorOpenLiveConsole(_ context.Context, _ string, _, _ int, params map[string]any) (*connectorapi.LiveConsoleSession, error) {
	gateway.openCalls++
	gateway.lastParams = params
	return &connectorapi.LiveConsoleSession{}, nil
}
