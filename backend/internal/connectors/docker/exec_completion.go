package dockerconnector

import (
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func dockerExecShell(command string) (script, marker string) {
	marker = "__AIPERMISSION_DOCKER_EXIT_" + rand.Text() + "__"
	// Keep the user command in a child shell: its exit/exec cannot skip the
	// completion record owned by the outer shell. This is observation, not a sandbox.
	script = "sh -lc " + shellQuote(command) + "; code=$?; printf '\\n" + marker + "%s\\n' \"$code\"; exit \"$code\""
	return script, marker
}

func observeDockerExecResult(result connectors.CommandRunResult, marker string) (connectors.CommandRunResult, error) {
	prefix := "\n" + marker
	index := strings.LastIndex(result.Stdout, prefix)
	if index < 0 {
		// A successful Docker CLI response already confirms completion. A nonzero
		// response without the inner exit record cannot distinguish reply loss.
		return result, dockerCLIOutcomeError("docker exec", result)
	}
	frame := strings.TrimSuffix(result.Stdout[index+len(prefix):], "\n")
	exitCode, err := strconv.Atoi(frame)
	if err != nil || exitCode < 0 || exitCode > 255 || strconv.Itoa(exitCode) != frame || exitCode != result.ExitCode || !strings.HasSuffix(result.Stdout, "\n") {
		return result, dockerUnconfirmedCLIError("docker exec", result)
	}
	result.Stdout = result.Stdout[:index]
	return result, nil
}

func dockerCLIOutcomeError(operation string, result connectors.CommandRunResult) error {
	if !result.DispatchStarted || result.ExitCode == 0 {
		return nil
	}
	return dockerUnconfirmedCLIError(operation, result)
}

func dockerUnconfirmedCLIError(operation string, result connectors.CommandRunResult) error {
	err := fmt.Errorf("%s completion could not be confirmed; inspect container state before retrying", operation)
	if !result.DispatchStarted {
		return err
	}
	return connectors.ClassifyOutcomeUnknown("docker_cli_response", map[string]any{
		"exit_code": result.ExitCode, "output_withheld": true,
	}, err)
}
