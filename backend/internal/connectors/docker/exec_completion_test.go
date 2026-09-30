package dockerconnector

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestDockerExecCompletionPreservesChildExitAndExactOutput(t *testing.T) {
	requireLocalPOSIXShell(t)
	for _, test := range []struct {
		name, command, output string
		exitCode              int
	}{
		{"success", "printf hi", "hi", 0},
		{"ordinary failure with EOF", "printf 'EOF\\n'; exit 1", "EOF\n", 1},
		{"explicit exit", "exit 23", "", 23},
		{"exec", "exec sh -c 'printf replaced; exit 7'", "replaced", 7},
		{"signal", "kill -TERM $$", "Terminated\n", 143},
		{"quoting and multiline", "printf '%s\\n' \"a'b\"\nprintf '%s' '$HOME;literal'", "a'b\n$HOME;literal", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			script, marker := dockerExecShell(test.command)
			output, err := exec.CommandContext(t.Context(), "/bin/sh", "-c", script).CombinedOutput()
			exitCode := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatal(err)
				}
				exitCode = exitErr.ExitCode()
			}
			result, err := observeDockerExecResult(connectors.CommandRunResult{Stdout: string(output), ExitCode: exitCode, DispatchStarted: true}, marker)
			if err != nil || (test.name != "signal" && result.Stdout != test.output) || result.ExitCode != test.exitCode || strings.Contains(result.Stdout, marker) {
				t.Fatalf("observed result = %#v, %v; raw=%q", result, err, output)
			}
		})
	}
}

func TestDockerExecSuccessfulCLIResponseRemainsConfirmedWithoutCapturedRecord(t *testing.T) {
	_, marker := dockerExecShell("printf hi")
	for _, output := range []string{"hi", strings.Repeat("x", maxExecOutputBytes), "\n" + marker[:len(marker)-3]} {
		result, err := observeDockerExecResult(connectors.CommandRunResult{DispatchStarted: true, Stdout: output}, marker)
		if err != nil || result.Stdout != output || result.ExitCode != 0 {
			t.Fatalf("confirmed CLI success rejected: exit=%d error=%v", result.ExitCode, err)
		}
	}
	_, err := observeDockerExecResult(connectors.CommandRunResult{DispatchStarted: true, ExitCode: 1, Stdout: strings.Repeat("x", maxExecOutputBytes)}, marker)
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		t.Fatalf("truncated nonzero response treated as definitive: %v", err)
	}
}

func TestDockerExecFullySerializedCommandPreservesOptionsAndPayload(t *testing.T) {
	requireLocalPOSIXShell(t)
	directory := t.TempDir()
	mock := filepath.Join(directory, "docker")
	// Validate actual shell arguments, then run the command as a disposable CLI
	// stand-in. No Docker socket or real container is involved.
	if err := os.WriteFile(mock, []byte("#!/bin/sh\n[ \"$1\" = exec ] && [ \"$2\" = --user ] && [ \"$3\" = \"app'user\" ] && [ \"$4\" = --workdir ] && [ \"$5\" = \"/tmp/a'b c\" ] && [ \"$6\" = -- ] && [ \"$7\" = 111111111111 ] || exit 99\nshift 7\nexec \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	transport := newMutationReplyTransport(func(request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
		command := exec.CommandContext(t.Context(), "/bin/sh", "-c", request.Command)
		command.Env = append(os.Environ(), "PATH="+directory+":"+os.Getenv("PATH"))
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("serialized exec failed: %v, %s", err, output)
		}
		return connectors.CommandRunResult{DispatchStarted: true, Stdout: string(output)}, nil
	})
	runtime := connectors.RuntimeContext{Target: dockerTarget(), Profile: dockerProfile("selected"), Capabilities: fakeCapabilities{transport: transport}}
	result, err := New().ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionContainerExec, Payload: map[string]any{
		"container": "api", "user": "app'user", "workdir": "/tmp/a'b c", "command": "printf '%s' \"a'b;$(printf c)\"",
	}})
	if err != nil || result.Status != connectors.ResultCompleted || result.DisplayText != "a'b;c" || strings.Contains(result.DisplayText, "AIPERMISSION_DOCKER_EXIT") {
		t.Fatalf("fully serialized output = %#v, %v", result, err)
	}
}

func requireLocalPOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local shell execution fixture requires POSIX; completion parsing remains platform-independent")
	}
}

func TestDockerExecMissingOrInvalidCompletionFailsClosed(t *testing.T) {
	const marker = "__AIPERMISSION_DOCKER_EXIT_TEST__"
	for _, test := range []struct {
		name, output string
		exitCode     int
	}{
		{"reply lost", "Docker API: EOF", 1},
		{"completion truncated", "\n" + marker + "1", 1},
		{"mismatched exit", "\n" + marker + "0\n", 1},
		{"mismatch despite CLI success", "\n" + marker + "1\n", 0},
		{"extra daemon output", "\n" + marker + "1\nconnection reset\n", 1},
		{"bad exit", "\n" + marker + "999\n", 1},
		{"foreign completion", "\nOTHER_CALL1\n", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := observeDockerExecResult(connectors.CommandRunResult{Stdout: test.output, ExitCode: test.exitCode, DispatchStarted: true}, marker)
			if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorDetails(err)["output_withheld"] != true {
				t.Fatalf("invalid completion was treated as definitive: %v", err)
			}
		})
	}
}

func TestDockerExecCompletionMarkerIsPerInvocation(t *testing.T) {
	_, first := dockerExecShell("exit 1")
	_, second := dockerExecShell("exit 1")
	if first == second {
		t.Fatal("exec completion identities were reused")
	}
	_, err := observeDockerExecResult(connectors.CommandRunResult{DispatchStarted: true, ExitCode: 1, Stdout: "\n" + first + "1\n"}, second)
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		t.Fatalf("foreign invocation completion was accepted: %v", err)
	}
}
