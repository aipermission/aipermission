package kubernetesconnector

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestKubectlCommandValidation(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
		valid   bool
	}{
		{name: "default", want: "kubectl", valid: true},
		{name: "binary", command: "kubectl", want: "kubectl", valid: true},
		{name: "wrapper path", command: "/usr/local/bin/kubectl-wrapper", want: "/usr/local/bin/kubectl-wrapper", valid: true},
		{name: "separator", command: "kubectl; id"},
		{name: "command substitution", command: "kubectl$(id)"},
		{name: "pipe", command: "kubectl | cat"},
		{name: "redirect", command: "kubectl>/tmp/output"},
		{name: "newline", command: "kubectl\nid"},
		{name: "arguments", command: "sudo kubectl"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, err := KubectlCommand(connectors.TargetView{Config: map[string]any{"kubectl_command": test.command}})
			if test.valid {
				if err != nil || command != test.want {
					t.Fatalf("command = %q, err = %v, want %q", command, err, test.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), ErrInvalidConfig.Error()) {
				t.Fatalf("expected invalid config error, got command=%q err=%v", command, err)
			}
		})
	}
}

func TestValidateTargetConfigRejectsUnsafeKubectlCommand(t *testing.T) {
	err := New().ValidateTargetConfig(map[string]any{"kubectl_command": "kubectl `id`"})
	if err == nil || !strings.Contains(err.Error(), ErrInvalidConfig.Error()) {
		t.Fatalf("expected invalid target config, got %v", err)
	}
}

func TestExecuteActionRejectsUnsafeKubectlCommandBeforeTransport(t *testing.T) {
	target := kubeTarget()
	target.Config["kubectl_command"] = "kubectl && id"
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target:       target,
		Profile:      kubeProfile("selected"),
		Capabilities: fakeCapabilities{transport: &fakeCommandTransport{}},
	}, connectors.PreparedAction{ActionName: ActionListPods, Payload: map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), ErrInvalidConfig.Error()) {
		t.Fatalf("expected invalid config error, got %v", err)
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

func TestRolloutRestartUsesResourceVersionPrecondition(t *testing.T) {
	transport := &fakeCommandTransport{fallback: connectors.CommandRunResult{Stdout: `{"kind":"Deployment"}`, ExitCode: 0, DurationMS: 8}}
	result, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target: kubeTarget(), Profile: kubeProfile("selected"), Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionRolloutRestart, Payload: map[string]any{
		"namespace": "production", "deployment": "api", "expected_resource_version": "12345",
	}})
	if err != nil {
		t.Fatalf("conditional rollout restart: %v", err)
	}
	if len(transport.commands) != 1 {
		t.Fatalf("commands = %#v", transport.commands)
	}
	command := transport.commands[0]
	if !strings.Contains(command, "kubectl patch deployment 'api'") || !strings.Contains(command, `"resourceVersion":"12345"`) || strings.Contains(command, "rollout restart") {
		t.Fatalf("conditional command = %q", command)
	}
	output := result.Output.(map[string]any)
	if output["expected_resource_version"] != "12345" {
		t.Fatalf("output = %#v", output)
	}
}

func TestConditionalRolloutRestartPatchChangesWithinOneSecond(t *testing.T) {
	firstTime := time.Date(2026, time.September, 4, 12, 0, 0, 123, time.UTC)
	secondTime := firstTime.Add(time.Nanosecond)
	first, err := conditionalRolloutRestartPatch("12345", firstTime)
	if err != nil {
		t.Fatal(err)
	}
	second, err := conditionalRolloutRestartPatch("12345", secondTime)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("same-second restart patches must differ: %s", first)
	}
	if !strings.Contains(first, `"2026-09-04T12:00:00.000000123Z"`) ||
		!strings.Contains(second, `"2026-09-04T12:00:00.000000124Z"`) {
		t.Fatalf("restart patches lost nanosecond precision: first=%s second=%s", first, second)
	}
}

func TestPrepareRolloutRestartPublishesConditionalRetryPolicy(t *testing.T) {
	connector := New()
	for _, test := range []struct {
		name  string
		input map[string]any
	}{
		{name: "unguarded", input: map[string]any{"namespace": "production", "deployment": "api"}},
		{name: "guarded", input: map[string]any{"namespace": "production", "deployment": "api", "expected_resource_version": "12345"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := connector.PrepareAction(context.Background(), connectors.ActionRequest{
				Target: kubeTarget(), Profile: kubeProfile("selected"), ActionName: ActionRolloutRestart, Input: test.input,
			})
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if test.name == "unguarded" && prepared.RetryPolicy != nil {
				t.Fatalf("unguarded rollout retry policy = %#v, want catalog non_idempotent policy", prepared.RetryPolicy)
			}
			if test.name == "guarded" && (prepared.RetryPolicy == nil || prepared.RetryPolicy.Class != connectors.RetryConditional || !reflect.DeepEqual(prepared.RetryPolicy.PreconditionFields, []string{"expected_resource_version"})) {
				t.Fatalf("guarded rollout retry policy = %#v", prepared.RetryPolicy)
			}
		})
	}
}

func TestRolloutRestartFailsClosedAfterAmbiguousKubectlExit(t *testing.T) {
	for _, test := range []struct {
		name       string
		stderr     string
		wantStatus connectors.ResultStatus
	}{
		{name: "plain EOF", stderr: "EOF", wantStatus: connectors.ResultOutcomeUnknown},
		{name: "gateway timeout", stderr: "Error from server (GatewayTimeout): upstream timed out", wantStatus: connectors.ResultOutcomeUnknown},
		{name: "server timeout", stderr: "Error from server (Timeout): request timed out", wantStatus: connectors.ResultOutcomeUnknown},
		{name: "forbidden", stderr: "Error from server (Forbidden): deployments is forbidden", wantStatus: connectors.ResultFailed},
		{name: "not found", stderr: "Error from server (NotFound): deployments.apps api not found", wantStatus: connectors.ResultFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &fakeCommandTransport{fallback: connectors.CommandRunResult{Stderr: test.stderr, ExitCode: 1, DispatchStarted: true}}
			_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
				Target: kubeTarget(), Profile: kubeProfile("selected"), Capabilities: fakeCapabilities{transport: transport},
			}, connectors.PreparedAction{ActionName: ActionRolloutRestart, Payload: map[string]any{"namespace": "production", "deployment": "api"}})
			if got := connectors.ErrorStatus(err); got != test.wantStatus && !(test.wantStatus == connectors.ResultFailed && got == "") {
				t.Fatalf("error = %v status=%q want=%q", err, got, test.wantStatus)
			}
		})
	}
}

func TestRolloutRestartClassifiesTransportFailureAsOutcomeUnknown(t *testing.T) {
	transport := &fakeCommandTransport{
		errorResult: connectors.CommandRunResult{DispatchStarted: true},
		err:         errors.New("connection reset after dispatch"),
	}
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target: kubeTarget(), Profile: kubeProfile("selected"), Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionRolloutRestart, Payload: map[string]any{
		"namespace": "production", "deployment": "api", "expected_resource_version": "12345",
	}})
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorCode(err) != "outcome_unknown" {
		t.Fatalf("error = %v, code = %q, status = %q", err, connectors.ErrorCode(err), connectors.ErrorStatus(err))
	}
}

func TestRolloutRestartPreservesPreDispatchTransportFailure(t *testing.T) {
	transportErr := errors.New("ssh dial refused before dispatch")
	transport := &fakeCommandTransport{err: transportErr}
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target: kubeTarget(), Profile: kubeProfile("selected"), Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionRolloutRestart, Payload: map[string]any{
		"namespace": "production", "deployment": "api", "expected_resource_version": "12345",
	}})
	if !errors.Is(err, transportErr) {
		t.Fatalf("error = %v, want original pre-dispatch failure", err)
	}
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		t.Fatalf("pre-dispatch error was misclassified as outcome unknown: %v", err)
	}
}

func TestRolloutRestartClassifiesResourceVersionConflict(t *testing.T) {
	transport := &fakeCommandTransport{fallback: connectors.CommandRunResult{Stderr: "Error from server (Conflict): Operation cannot be fulfilled: the object has been modified", ExitCode: 1}}
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target: kubeTarget(), Profile: kubeProfile("selected"), Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionRolloutRestart, Payload: map[string]any{
		"namespace": "production", "deployment": "api", "expected_resource_version": "12345",
	}})
	if connectors.ErrorCode(err) != "precondition_failed" {
		t.Fatalf("error = %v, code = %q", err, connectors.ErrorCode(err))
	}
}

func TestRolloutRestartClassifiesKubectlTransportExitAsOutcomeUnknown(t *testing.T) {
	transport := &fakeCommandTransport{fallback: connectors.CommandRunResult{
		Stderr:          "error: unexpected EOF while reading API response",
		ExitCode:        1,
		DispatchStarted: true,
	}}
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target: kubeTarget(), Profile: kubeProfile("selected"), Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionRolloutRestart, Payload: map[string]any{
		"namespace": "production", "deployment": "api",
	}})
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorCode(err) != "outcome_unknown" {
		t.Fatalf("error = %v, code = %q, status = %q", err, connectors.ErrorCode(err), connectors.ErrorStatus(err))
	}
}

func TestRolloutRestartDoesNotTreatIncidentalConflictTextAsDefiniteResponse(t *testing.T) {
	transport := &fakeCommandTransport{fallback: connectors.CommandRunResult{
		Stderr:          "transport conflict while reading response: unexpected EOF",
		ExitCode:        1,
		DispatchStarted: true,
	}}
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target: kubeTarget(), Profile: kubeProfile("selected"), Capabilities: fakeCapabilities{transport: transport},
	}, connectors.PreparedAction{ActionName: ActionRolloutRestart, Payload: map[string]any{
		"namespace": "production", "deployment": "api", "expected_resource_version": "12345",
	}})
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorCode(err) != "outcome_unknown" {
		t.Fatalf("incidental conflict text was treated as definite: %v", err)
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
	results     map[string]connectors.CommandRunResult
	fallback    connectors.CommandRunResult
	errorResult connectors.CommandRunResult
	commands    []string
	err         error
}

func (transport *fakeCommandTransport) ConnectorRuntimeCapability() string {
	return connectors.CommandTransportCapabilityName
}

func (transport *fakeCommandTransport) RunConnectorCommand(_ context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	transport.commands = append(transport.commands, request.Command)
	if transport.err != nil {
		return transport.errorResult, transport.err
	}
	result, ok := transport.results[request.Command]
	if !ok {
		if transport.fallback.Stdout != "" || transport.fallback.Stderr != "" || transport.fallback.ExitCode != 0 || transport.fallback.DurationMS != 0 {
			return transport.fallback, nil
		}
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
