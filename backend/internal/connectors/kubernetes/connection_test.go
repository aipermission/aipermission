package kubernetesconnector

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestConnectionProbeFailureClassification(t *testing.T) {
	for _, test := range []struct {
		name   string
		result connectors.CommandRunResult
		err    error
		status connectors.TestStatus
	}{
		{name: "permission", result: connectors.CommandRunResult{ExitCode: 1, Stderr: "Forbidden"}, status: connectors.TestFailedPermission},
		{name: "canceled transport", err: context.Canceled, status: connectors.TestFailedNetwork},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &fakeCommandTransport{fallback: test.result, err: test.err}
			result, err := New().TestConnection(context.Background(), connectors.RuntimeContext{
				Target: kubeTarget(), Profile: kubeProfile("selected"), Capabilities: fakeCapabilities{transport: transport},
			})
			if err != nil || result.Status != test.status || len(transport.commands) != 1 {
				t.Fatalf("result=%#v err=%v commands=%#v", result, err, transport.commands)
			}
		})
	}
}

func TestConnectionRejectsInvalidSelectedScopeBeforeTransport(t *testing.T) {
	for _, namespaces := range []string{"", "--all-namespaces", "../../other"} {
		t.Run(namespaces, func(t *testing.T) {
			profile := kubeProfile("selected")
			profile.Public["namespaces"] = namespaces
			transport := &fakeCommandTransport{}
			result, err := New().TestConnection(context.Background(), connectors.RuntimeContext{
				Target: kubeTarget(), Profile: profile, Capabilities: fakeCapabilities{transport: transport},
			})
			if err != nil || result.Status != connectors.TestUnknownError || len(transport.commands) != 0 {
				t.Fatalf("invalid scope dispatched: result=%#v err=%v commands=%#v", result, err, transport.commands)
			}
		})
	}
}
