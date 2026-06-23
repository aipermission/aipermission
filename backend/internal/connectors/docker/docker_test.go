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

func TestContainerExecRunsBoundedCommandInsideScopedContainer(t *testing.T) {
	transport := &fakeCommandTransport{
		results: map[string]connectors.CommandRunResult{
			"docker ps -a --no-trunc --format '{{json .}}'": {Stdout: `{"ID":"111111111111","Names":"api","Image":"app:latest","State":"running","Status":"Up 1 hour"}`},
			"docker exec -- 'api' sh -lc 'printf hi' 2>&1":  {Stdout: "hi", ExitCode: 0, DurationMS: 7},
		},
	}
	result, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target:       dockerTarget(),
		Profile:      dockerProfile("selected"),
		Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionContainerExec, Payload: map[string]any{"container": "api", "command": "printf hi"}})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.Status != connectors.ResultCompleted || result.DisplayText != "hi" {
		t.Fatalf("unexpected exec result: %#v", result)
	}
	output, _ := result.Output.(map[string]any)
	if output["exit_code"] != 0 || output["container"] == nil {
		t.Fatalf("unexpected exec output: %#v", output)
	}
}

func TestContainerExecReturnsFailedStatusForNonZeroExit(t *testing.T) {
	transport := &fakeCommandTransport{
		results: map[string]connectors.CommandRunResult{
			"docker ps -a --no-trunc --format '{{json .}}'":   {Stdout: `{"ID":"111111111111","Names":"api","Image":"app:latest","State":"running","Status":"Up 1 hour"}`},
			"docker exec -- 'api' sh -lc 'cat /missing' 2>&1": {Stdout: "missing\n", ExitCode: 1},
		},
	}
	result, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target:       dockerTarget(),
		Profile:      dockerProfile("selected"),
		Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionContainerExec, Payload: map[string]any{"container": "api", "command": "cat /missing"}})
	if err != nil {
		t.Fatalf("exec failed result should not be transport error: %v", err)
	}
	if result.Status != connectors.ResultFailed || !strings.Contains(result.Error, "code 1") {
		t.Fatalf("unexpected failed exec result: %#v", result)
	}
}

func TestListImagesFiltersBySelectedScope(t *testing.T) {
	transport := &fakeCommandTransport{
		results: map[string]connectors.CommandRunResult{
			"docker image ls --no-trunc --format '{{json .}}'": {Stdout: strings.Join([]string{
				`{"ID":"sha256:aaa","Repository":"app","Tag":"latest","Size":"100MB"}`,
				`{"ID":"sha256:bbb","Repository":"postgres","Tag":"16","Size":"400MB"}`,
			}, "\n")},
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
	}, connectors.PreparedAction{ActionName: ActionListImages, Payload: map[string]any{}})
	if err != nil {
		t.Fatalf("list images: %v", err)
	}
	output, _ := result.Output.(map[string]any)
	images, _ := output["images"].([]DockerImage)
	if len(images) != 1 || images[0].Ref() != "app:latest" || images[0].Containers != 1 {
		t.Fatalf("unexpected scoped images: %#v", images)
	}
}

func TestListNetworksAndVolumesUseScopedInspect(t *testing.T) {
	transport := &fakeCommandTransport{
		results: map[string]connectors.CommandRunResult{
			"docker ps -a --no-trunc --format '{{json .}}'": {Stdout: strings.Join([]string{
				`{"ID":"111111111111","Names":"api","Image":"app:latest","State":"running","Status":"Up 1 hour (healthy)","Labels":"com.docker.compose.project=watb,com.docker.compose.service=api"}`,
				`{"ID":"222222222222","Names":"db","Image":"postgres:16","State":"running","Status":"Up 1 hour"}`,
			}, "\n")},
			"docker inspect -- 'api'": {Stdout: `[{
				"NetworkSettings":{"Networks":{"frontend":{"NetworkID":"net1","Driver":"bridge"}}},
				"Mounts":[{"Type":"volume","Name":"api-data","Driver":"local","Source":"/var/lib/docker/volumes/api-data/_data"}]
			}]`},
		},
	}
	runtime := connectors.RuntimeContext{
		Target:       dockerTarget(),
		Profile:      dockerProfile("selected"),
		Capabilities: fakeCapabilities{transport: transport},
	}
	networkResult, err := New().ExecuteAction(context.Background(), runtime, connectors.PreparedAction{ActionName: ActionListNetworks, Payload: map[string]any{}})
	if err != nil {
		t.Fatalf("list networks: %v", err)
	}
	networkOutput, _ := networkResult.Output.(map[string]any)
	networks, _ := networkOutput["networks"].([]DockerNetwork)
	if len(networks) != 1 || networks[0].Name != "frontend" || networks[0].Containers != 1 {
		t.Fatalf("unexpected scoped networks: %#v", networks)
	}

	volumeResult, err := New().ExecuteAction(context.Background(), runtime, connectors.PreparedAction{ActionName: ActionListVolumes, Payload: map[string]any{}})
	if err != nil {
		t.Fatalf("list volumes: %v", err)
	}
	volumeOutput, _ := volumeResult.Output.(map[string]any)
	volumes, _ := volumeOutput["volumes"].([]DockerVolume)
	if len(volumes) != 1 || volumes[0].Name != "api-data" || volumes[0].Containers != 1 {
		t.Fatalf("unexpected scoped volumes: %#v", volumes)
	}

	containers, err := parseDockerPS(transport.results["docker ps -a --no-trunc --format '{{json .}}'"].Stdout)
	if err != nil {
		t.Fatalf("parse containers: %v", err)
	}
	if containers[0].Health != "healthy" || containers[0].ComposeProject != "watb" || containers[0].ComposeService != "api" {
		t.Fatalf("expected enriched compose metadata, got %#v", containers[0])
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
