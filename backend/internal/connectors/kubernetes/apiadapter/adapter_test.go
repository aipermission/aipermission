package apiadapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	kubernetesconnector "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

func TestOpenLiveConsoleOmitsMissingOrNilContainer(t *testing.T) {
	for _, nilContainer := range []bool{false, true} {
		params := map[string]any{"namespace": " default ", "pod": " api-123 "}
		if nilContainer {
			params["container"] = nil
		}
		runtime := fakeLiveConsoleRuntime{
			target:  connectors.TargetView{Config: map[string]any{"transport_target_ref": "ssh:2:20"}},
			profile: connectors.CredentialProfileView{Public: map[string]any{"scope_mode": "all"}},
		}
		gateway := &fakeLiveConsoleGateway{}
		_, err := adapter{}.OpenLiveConsole(t.Context(), gateway, runtime, connectorapi.LiveConsoleOpenRequest{RuntimeID: 7, Params: params})
		if err != nil || gateway.openCalls != 1 {
			t.Fatalf("nil container=%t: calls=%d error=%v", nilContainer, gateway.openCalls, err)
		}
		command, ok := gateway.lastParams["force_shell_command"].(string)
		if !ok || strings.Contains(command, " -c ") || strings.Contains(command, "<nil>") || !strings.Contains(command, "-n 'default' 'api-123'") {
			t.Fatalf("optional container changed command: %q", command)
		}
	}
}

func TestKubectlExecShellCommandPreservesContextAndShell(t *testing.T) {
	got, err := kubectlExecShellCommand(connectors.TargetView{Config: map[string]any{
		"context": "team's cluster", "kubectl_command": "/usr/local/bin/kubectl",
	}}, "default", "api-123", "web")
	want := "'/usr/local/bin/kubectl' --context 'team'\"'\"'s cluster' exec -it -n 'default' 'api-123' -c 'web' -- sh -lc 'if command -v bash >/dev/null 2>&1; then exec bash -l; fi; exec sh'"
	if err != nil || got != want {
		t.Fatalf("command=%q error=%v, want %q", got, err, want)
	}
}

func TestKubectlExecShellCommandOmitsEmptyContainer(t *testing.T) {
	command, err := kubectlExecShellCommand(connectors.TargetView{
		Config: map[string]any{"kubectl_command": "kubectl"},
	}, "default", "api-123", "")
	if err != nil {
		t.Fatalf("build command: %v", err)
	}
	if strings.Contains(command, " -c ") {
		t.Fatalf("empty container should not add -c: %s", command)
	}
	if strings.Contains(command, "<nil>") {
		t.Fatalf("command should not include nil marker: %s", command)
	}
	if !strings.Contains(command, "exec -it -n 'default' 'api-123' -- sh -lc") {
		t.Fatalf("unexpected kubectl exec command: %s", command)
	}
}

func TestKubectlExecShellCommandRejectsUnsafeConfiguredCommand(t *testing.T) {
	_, err := kubectlExecShellCommand(connectors.TargetView{
		Config: map[string]any{"kubectl_command": "kubectl; curl example.invalid"},
	}, "default", "api-123", "")
	if err == nil || !strings.Contains(err.Error(), "kubectl_command") {
		t.Fatalf("expected unsafe command error, got %v", err)
	}
}

func TestOpenLiveConsoleRejectsFlagLikeNamesBeforeGateway(t *testing.T) {
	target := connectors.TargetView{
		ID: 1, Ref: "kubernetes:1:10", ConnectorKind: kubernetesconnector.Kind,
		Config: map[string]any{"transport_target_ref": "ssh:2:20", "connection_mode": "over_ssh"},
	}
	profile := connectors.CredentialProfileView{
		ID: 10, TargetID: 1, ConnectorKind: kubernetesconnector.Kind,
		Public: map[string]any{"scope_mode": "all"},
	}
	runtime := fakeLiveConsoleRuntime{target: target, profile: profile}

	for _, test := range []struct {
		name   string
		params map[string]any
	}{
		{name: "namespace", params: map[string]any{"namespace": "--all-namespaces", "pod": "api"}},
		{name: "pod", params: map[string]any{"namespace": "default", "pod": "--help"}},
		{name: "container", params: map[string]any{"namespace": "default", "pod": "api", "container": "--help"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway := &fakeLiveConsoleGateway{}
			_, err := adapter{}.OpenLiveConsole(t.Context(), gateway, runtime, connectorapi.LiveConsoleOpenRequest{
				RuntimeID: 7,
				Params:    test.params,
			})
			if err == nil || !strings.Contains(err.Error(), "invalid kubernetes object name") {
				t.Fatalf("expected invalid object name error, got %v", err)
			}
			if gateway.openCalls != 0 {
				t.Fatalf("gateway open calls = %d, want none", gateway.openCalls)
			}
		})
	}
}

type fakeLiveConsoleRuntime struct {
	target  connectors.TargetView
	profile connectors.CredentialProfileView
}

func (runtime fakeLiveConsoleRuntime) TargetProfileByRuntimeID(context.Context, int64) (connectors.TargetView, connectors.CredentialProfileView, connectorapi.RuntimeSurface, error) {
	return runtime.target, runtime.profile, connectorapi.RuntimeSurface{
		ConnectorKind:  kubernetesconnector.Kind,
		CapabilityKind: "live_console",
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

func (fakeLiveConsoleRuntime) CredentialResources(string) resourcecontract.CredentialResourceStore {
	return nil
}

type fakeLiveConsoleGateway struct {
	openCalls  int
	lastParams map[string]any
}

func (*fakeLiveConsoleGateway) ConnectorTrustStorePath() string { return "" }

func (*fakeLiveConsoleGateway) ConnectorRunCommand(context.Context, connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	return connectors.CommandRunResult{}, errors.New("unexpected command")
}

func (gateway *fakeLiveConsoleGateway) ConnectorOpenLiveConsole(_ context.Context, _ string, _, _ int, params map[string]any) (*connectorapi.LiveConsoleSession, error) {
	gateway.openCalls++
	gateway.lastParams = params
	return &connectorapi.LiveConsoleSession{}, nil
}
