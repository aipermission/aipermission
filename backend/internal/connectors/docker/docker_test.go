package dockerconnector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestPrepareActionRejectsEmptyContainer(t *testing.T) {
	_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     dockerTarget(),
		Profile:    dockerProfile("selected"),
		ActionName: ActionContainerLogs,
		Input:      map[string]any{"container": " "},
	})
	if err == nil {
		t.Fatal("expected empty container error")
	}
}

func TestExecuteActionFiltersContainersByProfileScope(t *testing.T) {
	transport := &fakeCommandTransport{
		results: map[string]connectors.CommandRunResult{
			"docker ps -a --no-trunc --format '{{json .}}'": {Stdout: strings.Join([]string{
				`{"ID":"111111111111","Names":"api","Image":"app:latest","State":"running","Status":"Up 1 hour"}`,
				`{"ID":"222222222222","Names":"db","Image":"postgres:16","State":"running","Status":"Up 1 hour"}`,
			}, "\n")},
		},
	}
	result, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target:       dockerTarget(),
		Profile:      dockerProfile("selected"),
		Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionListContainers, Payload: map[string]any{"all": true}})
	if err != nil {
		t.Fatalf("execute list: %v", err)
	}
	output, _ := result.Output.(map[string]any)
	containers, _ := output["containers"].([]DockerContainer)
	if len(containers) != 1 || containers[0].Name != "api" {
		t.Fatalf("unexpected containers: %#v", containers)
	}
}

func TestExecuteActionRejectsOutOfScopeContainer(t *testing.T) {
	transport := &fakeCommandTransport{
		results: map[string]connectors.CommandRunResult{
			"docker ps -a --no-trunc --format '{{json .}}'": {Stdout: strings.Join([]string{
				`{"ID":"111111111111","Names":"api","Image":"app:latest","State":"running","Status":"Up 1 hour"}`,
				`{"ID":"222222222222","Names":"db","Image":"postgres:16","State":"running","Status":"Up 1 hour"}`,
			}, "\n")},
		},
	}
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target:       dockerTarget(),
		Profile:      dockerProfile("selected"),
		Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionContainerLogs, Payload: map[string]any{"container": "db", "tail": 20}})
	if err == nil || !strings.Contains(err.Error(), ErrScopeDenied.Error()) {
		t.Fatalf("expected scope denied error, got %v", err)
	}
}

func TestInspectRedactsEnvironment(t *testing.T) {
	transport := &fakeCommandTransport{
		results: map[string]connectors.CommandRunResult{
			"docker ps -a --no-trunc --format '{{json .}}'": {Stdout: `{"ID":"111111111111","Names":"api","Image":"app:latest","State":"running","Status":"Up 1 hour"}`},
			"docker inspect -- 'api'":                       {Stdout: `[{"Config":{"Env":["TOKEN=secret","DEBUG=true"]}}]`},
		},
	}
	result, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target:       dockerTarget(),
		Profile:      dockerProfile("selected"),
		Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionInspectContainer, Payload: map[string]any{"container": "api"}})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if strings.Contains(toJSON(t, result.Output), "secret") {
		t.Fatalf("inspect output contains unredacted secret: %#v", result.Output)
	}
}

func TestLifecycleReturnsRefreshedContainerState(t *testing.T) {
	transport := &fakeCommandTransport{
		sequences: map[string][]connectors.CommandRunResult{
			"docker ps -a --no-trunc --format '{{json .}}'": {
				{Stdout: `{"ID":"111111111111","Names":"api","Image":"app:latest","State":"exited","Status":"Exited (0) 1 minute ago"}`},
				{Stdout: `{"ID":"111111111111","Names":"api","Image":"app:latest","State":"running","Status":"Up 2 seconds"}`},
			},
		},
		results: map[string]connectors.CommandRunResult{
			"docker start -- 'api' 2>&1": {Stdout: "api\n"},
		},
	}
	result, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target:       dockerTarget(),
		Profile:      dockerProfile("selected"),
		Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionStartContainer, Payload: map[string]any{"container": "api"}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	output, _ := result.Output.(map[string]any)
	container, _ := output["container"].(DockerContainer)
	if container.State != "running" {
		t.Fatalf("expected refreshed running state, got %#v", container)
	}
}

func dockerTarget() connectors.TargetView {
	return connectors.TargetView{
		ID:            1,
		Ref:           "docker:1:10",
		ConnectorKind: Kind,
		Name:          "docker-host",
		Config:        map[string]any{"connection_mode": "over_ssh", "transport_target_ref": "ssh:2:20", "docker_command": "docker"},
	}
}

func dockerProfile(scopeMode string) connectors.CredentialProfileView {
	return connectors.CredentialProfileView{
		ID:            10,
		TargetID:      1,
		ConnectorKind: Kind,
		Kind:          "container_scope",
		Label:         "api-only",
		Public:        map[string]any{"scope_mode": scopeMode, "allowed_containers": "api"},
	}
}

type fakeCapabilities struct {
	transport connectors.CommandTransport
}

func (capabilities fakeCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.CommandTransportCapabilityName {
		return capabilities.transport
	}
	return nil
}

type fakeCommandTransport struct {
	results   map[string]connectors.CommandRunResult
	sequences map[string][]connectors.CommandRunResult
	calls     map[string]int
}

func (transport *fakeCommandTransport) ConnectorRuntimeCapability() string {
	return connectors.CommandTransportCapabilityName
}

func (transport *fakeCommandTransport) RunConnectorCommand(_ context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	if sequence := transport.sequences[request.Command]; len(sequence) > 0 {
		if transport.calls == nil {
			transport.calls = map[string]int{}
		}
		index := transport.calls[request.Command]
		if index >= len(sequence) {
			index = len(sequence) - 1
		}
		transport.calls[request.Command]++
		return sequence[index], nil
	}
	result, ok := transport.results[request.Command]
	if !ok {
		return connectors.CommandRunResult{ExitCode: 127, Stderr: "unexpected command: " + request.Command}, nil
	}
	return result, nil
}

func toJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	return string(data)
}
