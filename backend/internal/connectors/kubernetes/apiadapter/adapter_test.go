package apiadapter

import (
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestStringParamMissingValueReturnsEmpty(t *testing.T) {
	if got := stringParam(map[string]any{"namespace": "default"}, "container"); got != "" {
		t.Fatalf("expected missing optional param to be empty, got %q", got)
	}
	if got := stringParam(map[string]any{"container": nil}, "container"); got != "" {
		t.Fatalf("expected nil optional param to be empty, got %q", got)
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
