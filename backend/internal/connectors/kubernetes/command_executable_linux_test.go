package kubernetesconnector

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestKubectlInvocationBypassesFunctionsAndKeepsAbsoluteWrapper(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "kubectl")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'validated-executable'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, configured := range []string{"", "kubectl", binary} {
		command, err := KubectlShellCommand(connectors.TargetView{Config: map[string]any{"kubectl_command": configured}})
		if err != nil {
			t.Fatal(err)
		}
		process := exec.CommandContext(t.Context(), "/bin/sh", "-c", "kubectl() { printf 'unexpected-function'; }; "+command)
		process.Env = []string{"PATH=" + directory}
		output, err := process.CombinedOutput()
		if err != nil || string(output) != "validated-executable" {
			t.Fatalf("invocation selected wrong executable: %q %v", output, err)
		}
	}
	if _, err := KubectlShellCommand(connectors.TargetView{Config: map[string]any{"kubectl_command": "./kubectl"}}); err == nil {
		t.Fatal("relative invocation was accepted")
	}
}
