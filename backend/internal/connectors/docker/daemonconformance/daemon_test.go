package daemonconformance_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	docker "github.com/aipermission/aipermission/backend/internal/connectors/docker"
)

func TestDockerGuestQualification(t *testing.T) {
	if os.Getenv("AIPERMISSION_DOCKER_GUEST") != "1" {
		t.Skip("owned no-NIC VM qualification is disabled")
	}
	if _, err := os.Stat("/run/aipermission-docker-fixture"); err != nil {
		t.Fatal("guest ownership marker is missing")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	runtime := connectors.RuntimeContext{
		Target:       connectors.TargetView{ID: 1, Ref: "docker:1:1", ConnectorKind: docker.Kind, Config: map[string]any{"connection_mode": "over_ssh", "transport_target_ref": "ssh:2:2", "docker_command": "/fixture/docker-contained"}},
		Profile:      connectors.CredentialProfileView{ID: 1, TargetID: 1, ConnectorKind: docker.Kind, Kind: "container_scope", Public: map[string]any{"scope_mode": "selected", "allowed_containers": "fixture-allowed"}},
		Capabilities: guestSSHTransport(t),
	}
	connector := docker.New()
	connection, err := connector.TestConnection(ctx, runtime)
	if err != nil || connection.Status != connectors.TestOK {
		t.Fatalf("real Docker client/server probe: %#v / %v", connection, err)
	}
	listed := guestAction(t, ctx, runtime, docker.ActionListContainers, map[string]any{"all": true})
	containers := listed.Output.(map[string]any)["containers"].([]docker.DockerContainer)
	if len(containers) != 1 || containers[0].Name != "fixture-allowed" || containers[0].State != "running" {
		t.Fatalf("Docker scope inventory: %#v", containers)
	}
	assertGuestDockerScope(t, ctx, runtime)
	inspected := guestAction(t, ctx, runtime, docker.ActionInspectContainer, map[string]any{"container": containers[0].ID})
	encoded, err := json.Marshal(inspected.Output)
	if err != nil || strings.Contains(string(encoded), "fixture-env-private") || !strings.Contains(string(encoded), "FIXTURE_SECRET=***") {
		t.Fatalf("real inspect environment was not redacted: %v", err)
	}
	logs := guestAction(t, ctx, runtime, docker.ActionContainerLogs, map[string]any{"container": "fixture-allowed", "tail": 1})
	if !strings.Contains(logs.DisplayText, "fixture-ready") {
		t.Fatal("real container logs were not returned")
	}
	for _, sample := range []struct {
		command, output string
		code            int
	}{
		{"printf fixture-ok", "fixture-ok", 0}, {"printf 'EOF\\n'; exit 23", "EOF\n", 23},
	} {
		result := guestAction(t, ctx, runtime, docker.ActionContainerExec, map[string]any{"container": "fixture-allowed", "command": sample.command})
		output := result.Output.(map[string]any)
		want := connectors.ResultCompleted
		if sample.code != 0 {
			want = connectors.ResultFailed
		}
		if result.Status != want || output["exit_code"] != sample.code || output["output"] != sample.output || strings.Contains(result.DisplayText, "__AIPERMISSION_DOCKER_EXIT_") {
			t.Fatalf("real Docker exec identity/completion: %#v", result)
		}
	}
	for _, lifecycle := range []struct{ action, state string }{{docker.ActionStopContainer, "exited"}, {docker.ActionStartContainer, "running"}} {
		input := map[string]any{"container": "fixture-allowed"}
		if lifecycle.action == docker.ActionStopContainer {
			input["timeout_seconds"] = 1
		}
		result := guestAction(t, ctx, runtime, lifecycle.action, input)
		if result.Status != connectors.ResultCompleted || guestContainerState(t, ctx, "fixture-allowed") != lifecycle.state {
			t.Fatalf("real Docker lifecycle %s did not persist", lifecycle.action)
		}
	}
	if guestContainerState(t, ctx, "fixture-denied") != "running" {
		t.Fatal("out-of-scope container was modified")
	}
}

func guestAction(t *testing.T, ctx context.Context, runtime connectors.RuntimeContext, name string, input map[string]any) connectors.ActionResult {
	t.Helper()
	connector := docker.New()
	actions, err := connector.GetActionList(ctx, runtime.Target, runtime.Profile)
	if err != nil {
		t.Fatal(err)
	}
	var advertised *connectors.ActionDefinition
	for index := range actions {
		if actions[index].Name == name {
			advertised = &actions[index]
			break
		}
	}
	if advertised == nil {
		t.Fatalf("Docker did not advertise %s", name)
	}
	prepared, err := connector.PrepareAction(ctx, connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: name, Input: input, Reason: "owned disposable daemon conformance"})
	if err != nil || prepared.ActionName != name || prepared.Risk != advertised.Risk {
		t.Fatalf("Docker preparation contract: %#v / %v", prepared, err)
	}
	result, err := connector.ExecuteAction(ctx, runtime, prepared)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("real Docker action %s: %s", name, result.Status)
	return result
}

func guestContainerState(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	output, err := exec.CommandContext(ctx, "/fixture/docker-contained", "inspect", "--format", "{{.State.Status}}", "--", name).Output()
	if err != nil {
		t.Fatalf("independent guest Docker readback: %v", err)
	}
	return strings.TrimSpace(string(output))
}

func assertGuestDockerScope(t *testing.T, ctx context.Context, runtime connectors.RuntimeContext) {
	t.Helper()
	id, err := exec.CommandContext(ctx, "/fixture/docker-contained", "inspect", "--format", "{{.Id}}", "--", "fixture-denied").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{"fixture-denied", strings.TrimSpace(string(id))} {
		prepared, err := docker.New().PrepareAction(ctx, connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: docker.ActionStopContainer, Input: map[string]any{"container": reference}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = docker.New().ExecuteAction(ctx, runtime, prepared)
		if !errors.Is(err, docker.ErrScopeDenied) {
			t.Fatalf("denied container %s was not scope-rejected: %v", reference, err)
		}
	}
	if guestContainerState(t, ctx, "fixture-denied") != "running" {
		t.Fatal("scope rejection changed denied container state")
	}
}
