package gatewayworkspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

const shutdownFailureEnvironment = "AIPERMISSION_TEST_WORKSPACE_SHUTDOWN_EARLY_FAILURE"

func validShutdownFailureOutput(output []byte) bool {
	return regexp.MustCompile(`\A--- FAIL: TestWorkspaceCloseBridgesWorkersBeforeNativeStorageRelease \([0-9.]+s\)\n[ \t]+shutdown_boundary_test.go:[0-9]+: intentional shutdown fixture failure\n[ \t]+shutdown_boundary_test.go:[0-9]+: shutdown fixture joined before native storage cleanup\nFAIL\n(?:coverage: [0-9.]+% of statements\n)?\z`).Match(output)
}

func TestWorkspaceShutdownFixtureJoinsAfterEarlyFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkspaceCloseBridgesWorkersBeforeNativeStorageRelease$", "-test.timeout=10s")
	command.Env = append(os.Environ(), shutdownFailureEnvironment+"=1")
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || ctx.Err() != nil {
		t.Fatalf("intentional fixture failure did not drain: error=%v context=%v output=%s", err, ctx.Err(), output)
	}
	if !validShutdownFailureOutput(output) {
		t.Fatalf("failure-path fixture cleanup lost worker ownership: %s", output)
	}
}

func TestShutdownFailureOracleRejectsAdditionalAssertions(t *testing.T) {
	output := "--- FAIL: TestWorkspaceCloseBridgesWorkersBeforeNativeStorageRelease (0.001s)\n" +
		"    shutdown_boundary_test.go:1: intentional shutdown fixture failure\n" +
		"    shutdown_boundary_test.go:2: shutdown fixture joined before native storage cleanup\nFAIL\n"
	if !validShutdownFailureOutput([]byte(output)) {
		t.Fatal("oracle rejected the one intentional failure")
	}
	if !validShutdownFailureOutput([]byte(output + "coverage: 11.1% of statements\n")) {
		t.Fatal("oracle rejected native coverage instrumentation")
	}
	if validShutdownFailureOutput([]byte(output + "coverage: 11.1% of statements\nadditional assertion failure\n")) {
		t.Fatal("coverage instrumentation masked an additional diagnostic")
	}
	for _, extra := range []string{"completion ran before native storage closed", "storage closed during actions/recover", "discard native workspace: error", "unexpected worker failure"} {
		mutant := strings.Replace(output, "FAIL\n", "    shutdown_boundary_test.go:3: "+extra+"\nFAIL\n", 1)
		if validShutdownFailureOutput([]byte(mutant)) {
			t.Errorf("oracle masked additional failure %q", extra)
		}
	}
	for _, missing := range []string{"    shutdown_boundary_test.go:1: intentional shutdown fixture failure\n", "    shutdown_boundary_test.go:2: shutdown fixture joined before native storage cleanup\n"} {
		if validShutdownFailureOutput([]byte(strings.Replace(output, missing, "", 1))) {
			t.Error("oracle accepted a missing lifecycle assertion")
		}
	}
}
