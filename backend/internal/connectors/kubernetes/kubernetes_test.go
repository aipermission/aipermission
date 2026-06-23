package kubernetesconnector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestPrepareActionRejectsEmptyLogTarget(t *testing.T) {
	_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     kubeTarget(),
		Profile:    kubeProfile("selected"),
		ActionName: ActionLogs,
		Input:      map[string]any{"namespace": "production", "pod": " "},
	})
	if err == nil {
		t.Fatal("expected empty pod error")
	}
}

func TestListPodsUsesSelectedNamespaces(t *testing.T) {
	transport := &fakeCommandTransport{
		results: map[string]connectors.CommandRunResult{
			"kubectl get pods -n 'production' -o json": {Stdout: `{"items":[{"metadata":{"namespace":"production","name":"api","creationTimestamp":"2026-01-01T00:00:00Z"},"status":{"phase":"Running","containerStatuses":[{"ready":true,"restartCount":1}]},"spec":{"nodeName":"node-1"}}]}`},
		},
	}
	result, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target:       kubeTarget(),
		Profile:      kubeProfile("selected"),
		Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionListPods, Payload: map[string]any{}})
	if err != nil {
		t.Fatalf("list pods: %v", err)
	}
	output, _ := result.Output.(map[string]any)
	pods, _ := output["pods"].([]PodSummary)
	if len(pods) != 1 || pods[0].Namespace != "production" || pods[0].Ready != "1/1" || pods[0].Restarts != 1 {
		t.Fatalf("unexpected pods: %#v", pods)
	}
}

func TestLogsRejectsOutOfScopeNamespace(t *testing.T) {
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target:       kubeTarget(),
		Profile:      kubeProfile("selected"),
		Capabilities: fakeCapabilities{transport: &fakeCommandTransport{}},
	}, connectors.PreparedAction{ActionName: ActionLogs, Payload: map[string]any{"namespace": "staging", "pod": "api"}})
	if err == nil || !strings.Contains(err.Error(), ErrScopeDenied.Error()) {
		t.Fatalf("expected scope denied, got %v", err)
	}
}

func TestRolloutRestartRunsBoundedKubectlTemplate(t *testing.T) {
	transport := &fakeCommandTransport{
		results: map[string]connectors.CommandRunResult{
			"kubectl rollout restart deployment/'api' -n 'production' 2>&1": {Stdout: "deployment.apps/api restarted\n", ExitCode: 0, DurationMS: 9},
		},
	}
	result, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target:       kubeTarget(),
		Profile:      kubeProfile("selected"),
		Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionRolloutRestart, Payload: map[string]any{"namespace": "production", "deployment": "api"}})
	if err != nil {
		t.Fatalf("rollout restart: %v", err)
	}
	if result.Status != connectors.ResultCompleted || !strings.Contains(result.DisplayText, "restarted") {
		t.Fatalf("unexpected restart result: %#v", result)
	}
}

func TestDescribeReturnsResourceSummary(t *testing.T) {
	transport := &fakeCommandTransport{
		results: map[string]connectors.CommandRunResult{
			"kubectl get deployment 'api' -n 'production' -o json": {Stdout: `{"kind":"Deployment","metadata":{"namespace":"production","name":"api","creationTimestamp":"2026-01-01T00:00:00Z"}}`},
		},
	}
	result, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target:       kubeTarget(),
		Profile:      kubeProfile("selected"),
		Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionDescribe, Payload: map[string]any{"resource_type": "deployment", "namespace": "production", "name": "api"}})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if !strings.Contains(toJSON(t, result.Output), "Deployment") {
		t.Fatalf("unexpected describe output: %#v", result.Output)
	}
}

func kubeTarget() connectors.TargetView {
	return connectors.TargetView{
		ID:            1,
		Ref:           "kubernetes:1:10",
		ConnectorKind: Kind,
		Name:          "cluster",
		Config:        map[string]any{"connection_mode": "over_ssh", "transport_target_ref": "ssh:2:20", "kubectl_command": "kubectl"},
	}
}

func kubeProfile(scopeMode string) connectors.CredentialProfileView {
	return connectors.CredentialProfileView{
		ID:            10,
		TargetID:      1,
		ConnectorKind: Kind,
		Kind:          "namespace_scope",
		Label:         "production",
		Public:        map[string]any{"scope_mode": scopeMode, "namespaces": "production"},
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
	results map[string]connectors.CommandRunResult
}

func (transport *fakeCommandTransport) ConnectorRuntimeCapability() string {
	return connectors.CommandTransportCapabilityName
}

func (transport *fakeCommandTransport) RunConnectorCommand(_ context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
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
