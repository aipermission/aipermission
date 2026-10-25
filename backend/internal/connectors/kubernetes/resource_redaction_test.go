package kubernetesconnector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestDescribeRedactsInlineSecretsAtConnectorBoundary(t *testing.T) {
	const source = `{"kind":"Deployment","metadata":{"name":"api","namespace":"production","resourceVersion":"123","annotations":{"kubectl.kubernetes.io/last-applied-configuration":"not-even-json-with-private-credential","ordinary":"visible"}},"spec":{"template":{"spec":{"containers":[{"name":"api","env":[{"name":"API_TOKEN","value":"private-api-credential"},{"name":"REF","valueFrom":{"secretKeyRef":{"name":"credentials","key":"password"}}}]}],"initContainers":[{"env":[{"name":"INIT","value":"private-init-credential"}]}],"ephemeralContainers":[{"env":[{"name":"DEBUG","value":"private-debug-credential"}]}]}}}}`
	transport := &fakeCommandTransport{fallback: connectors.CommandRunResult{Stdout: source}}
	result, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target: kubeTarget(), Profile: kubeProfile("selected"), Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionDescribe, Payload: map[string]any{
		"resource_type": "deployment", "namespace": "production", "name": "api",
	}})
	if err != nil {
		t.Fatal(err)
	}
	output, err := json.Marshal(result.Output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "private-") || !strings.Contains(string(output), `"resourceVersion":"123"`) || !strings.Contains(string(output), `"ordinary":"visible"`) || !strings.Contains(string(output), `"secretKeyRef"`) {
		t.Fatalf("describe lost metadata or exposed inline credential: %s", output)
	}
}

func TestResourceRedactionPreservesInputAndMalformedMetadata(t *testing.T) {
	input := map[string]any{"env": []any{map[string]any{"name": "VALUE", "value": "private"}, "unexpected"}, "spec": map[string]any{"env": "invalid-but-visible"}}
	before, _ := json.Marshal(input)
	output := redactResource(input)
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("redaction mutated its source")
	}
	encoded, _ := json.Marshal(output)
	if strings.Contains(string(encoded), "private") || !strings.Contains(string(encoded), "invalid-but-visible") || !strings.Contains(string(encoded), "unexpected") {
		t.Fatalf("unexpected redaction: %s", encoded)
	}
}
