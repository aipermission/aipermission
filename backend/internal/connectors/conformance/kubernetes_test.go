package conformance_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	kubernetes "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
)

func TestKubernetesAPIRealService(t *testing.T) {
	requireProtocolFixture(t)
	observer := newKubeObserver(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	transport := &scopedSSHCommands{target: protocolSSHTarget(t)}
	client, err := execution.DialSSH(ctx, transport.target)
	if client != nil {
		_ = client.Close()
	}
	var unknown *execution.UnknownHostKeyError
	if !errors.As(err, &unknown) {
		t.Fatalf("owned SSH fixture did not require initial trust: %v", err)
	}
	assertOwnedSSHIdentity(t, unknown)
	if err := execution.TrustHostKey(transport.target.KnownHostsPath, "protocols:22", unknown.PublicKey); err != nil {
		t.Fatal(err)
	}
	runtime := connectors.RuntimeContext{
		Target:  connectors.TargetView{ID: 9, Ref: "kubernetes:9:9", ConnectorKind: kubernetes.Kind, Config: map[string]any{"connection_mode": "over_ssh", "transport_target_ref": "ssh:8:8", "kubectl_command": "/usr/local/bin/kubectl-contained"}},
		Profile: connectors.CredentialProfileView{ID: 9, TargetID: 9, ConnectorKind: kubernetes.Kind, Kind: "namespace_scope", Public: map[string]any{"scope_mode": "selected", "namespaces": "fixture-allowed"}},
		Secrets: fixtureSecrets{}, Capabilities: transport,
	}
	connector := kubernetes.New()
	assertConnection(t, connector, runtime)
	empty := executeAction(t, connector, runtime, kubernetes.ActionListPods, map[string]any{}).Output.(map[string]any)
	if empty["count"] != 0 {
		t.Fatalf("owned empty namespace returned pods: %#v", empty)
	}
	assertKubeRBACAndScope(t, ctx, connector, runtime, transport)
	seedKubeResources(t, ctx, observer)
	assertKubeDeniedMutation(t, ctx, observer)
	assertConnection(t, connector, runtime)
	for _, action := range []string{kubernetes.ActionListPods, kubernetes.ActionListWorkloads} {
		output := executeAction(t, connector, runtime, action, map[string]any{}).Output.(map[string]any)
		encoded, err := json.Marshal(output)
		if err != nil || output["count"] != 1 || !strings.Contains(string(encoded), "fixture-allowed") || strings.Contains(string(encoded), "fixture-denied") {
			t.Fatalf("real Kubernetes %s did not preserve namespace scope: %#v / %v", action, output, err)
		}
	}
	assertKubeConditionalRestart(t, ctx, connector, runtime, observer)
	assertKubeServerPatchWitnesses(t, ctx)
}

func assertKubeDeniedMutation(t *testing.T, ctx context.Context, observer kubeObserver) {
	t.Helper()
	path := "/apis/apps/v1/namespaces/fixture-denied/deployments/owned-deployment"
	before := observer.request(t, ctx, http.MethodGet, path, nil, http.StatusOK)
	observer.scoped(t).request(t, ctx, http.MethodPatch, path, map[string]any{
		"metadata": map[string]any{"annotations": map[string]any{"fixture-unauthorized": "must-not-apply"}},
	}, http.StatusForbidden)
	after := observer.request(t, ctx, http.MethodGet, path, nil, http.StatusOK)
	if resourceMetadata(t, before)["resourceVersion"] != resourceMetadata(t, after)["resourceVersion"] {
		t.Fatal("server RBAC rejected a mutation but the resource changed")
	}
}

func assertKubeRBACAndScope(t *testing.T, ctx context.Context, connector connectors.Connector, runtime connectors.RuntimeContext, transport *scopedSSHCommands) {
	t.Helper()
	private, err := transport.RunConnectorCommand(ctx, connectors.CommandRunRequest{SourceTargetRef: runtime.Target.Ref, Mode: "over_ssh", TransportTargetRef: "ssh:8:8", Command: "test ! -r /kube-material/observer-token", TimeoutSeconds: 15})
	if err != nil || private.ExitCode != 0 {
		t.Fatalf("SSH connector user can access elevated observer token: exit=%d / %v", private.ExitCode, err)
	}
	response, err := transport.RunConnectorCommand(ctx, connectors.CommandRunRequest{SourceTargetRef: runtime.Target.Ref, Mode: "over_ssh", TransportTargetRef: "ssh:8:8", Command: "/usr/local/bin/kubectl-contained get pods -n fixture-denied -o json", TimeoutSeconds: 15})
	if err != nil || response.ExitCode == 0 || !strings.Contains(response.Stderr, "Forbidden") {
		t.Fatalf("server RBAC did not independently refuse denied namespace: %#v / %v", response, err)
	}
	before := transport.calls
	prepared, err := connector.PrepareAction(ctx, connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: kubernetes.ActionListPods, Input: map[string]any{"namespace": "fixture-denied"}})
	if err != nil || transport.calls != before {
		t.Fatalf("connector preparation was not side-effect-free: %v", err)
	}
	_, err = connector.ExecuteAction(ctx, runtime, prepared)
	if !errors.Is(err, kubernetes.ErrScopeDenied) || transport.calls != before {
		t.Fatal("execution bypassed selected namespace admission")
	}
}

func seedKubeResources(t *testing.T, ctx context.Context, observer kubeObserver) {
	t.Helper()
	for _, namespace := range []string{"fixture-allowed", "fixture-denied"} {
		observer.request(t, ctx, http.MethodPost, "/api/v1/namespaces/"+namespace+"/pods", map[string]any{
			"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": "owned-pending"},
			"spec": map[string]any{"containers": []any{map[string]any{"name": "owned", "image": "fixture.invalid/not-pulled:owned"}}},
		}, http.StatusCreated)
		observer.request(t, ctx, http.MethodPost, "/apis/apps/v1/namespaces/"+namespace+"/deployments", map[string]any{
			"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "owned-deployment"},
			"spec": map[string]any{"replicas": 0, "selector": map[string]any{"matchLabels": map[string]any{"app": "owned"}}, "template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": "owned"}}, "spec": map[string]any{"containers": []any{map[string]any{"name": "owned", "image": "fixture.invalid/not-pulled:owned"}}},
			}},
		}, http.StatusCreated)
	}
}

func assertKubeConditionalRestart(t *testing.T, ctx context.Context, connector connectors.Connector, runtime connectors.RuntimeContext, observer kubeObserver) {
	t.Helper()
	path := "/apis/apps/v1/namespaces/fixture-allowed/deployments/owned-deployment"
	resource := observer.request(t, ctx, http.MethodGet, path, nil, http.StatusOK)
	version := resourceMetadata(t, resource)["resourceVersion"]
	input := map[string]any{"namespace": "fixture-allowed", "deployment": "owned-deployment", "expected_resource_version": version}
	executeAction(t, connector, runtime, kubernetes.ActionRolloutRestart, input)
	after := observer.request(t, ctx, http.MethodGet, path, nil, http.StatusOK)
	newVersion := resourceMetadata(t, after)["resourceVersion"]
	spec := after["spec"].(map[string]any)
	annotations := spec["template"].(map[string]any)["metadata"].(map[string]any)["annotations"].(map[string]any)
	if newVersion == version || annotations["kubectl.kubernetes.io/restartedAt"] == nil {
		t.Fatal("conditional restart was not applied to the real API")
	}
	prepared, err := connector.PrepareAction(ctx, connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: kubernetes.ActionRolloutRestart, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	_, err = connector.ExecuteAction(ctx, runtime, prepared)
	if err == nil || connectors.ErrorCode(err) != "precondition_failed" {
		t.Fatalf("real API stale version was not classified as precondition failure: %v", err)
	}
	readback := observer.request(t, ctx, http.MethodGet, path, nil, http.StatusOK)
	if resourceMetadata(t, readback)["resourceVersion"] != newVersion {
		t.Fatal("stale conditional restart changed the real resource")
	}
}
