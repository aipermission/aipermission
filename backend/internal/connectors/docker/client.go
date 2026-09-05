package dockerconnector

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type dockerClient struct {
	runtime   connectors.RuntimeContext
	transport connectors.CommandTransport
	command   string
	scope     dockerScope
}

func newDockerClient(runtime connectors.RuntimeContext) (*dockerClient, error) {
	transport, _ := runtime.Capability(connectors.CommandTransportCapabilityName).(connectors.CommandTransport)
	if transport == nil {
		return nil, ErrMissingTransport
	}
	command, err := DockerShellCommand(runtime.Target)
	if err != nil {
		return nil, err
	}
	return &dockerClient{
		runtime:   runtime,
		transport: transport,
		command:   command,
		scope:     dockerScopeFromProfile(runtime.Profile),
	}, nil
}

func (client *dockerClient) run(ctx context.Context, command string, timeoutSeconds int) (connectors.CommandRunResult, error) {
	return client.transport.RunConnectorCommand(ctx, connectors.CommandRunRequest{
		SourceTargetRef:    client.runtime.Target.Ref,
		Mode:               connectionMode(client.runtime.Target),
		TransportTargetRef: strings.TrimSpace(stringValue(client.runtime.Target.Config, "transport_target_ref")),
		Command:            command,
		TimeoutSeconds:     timeoutSeconds,
	})
}

func executeVersion(ctx context.Context, client *dockerClient) (connectors.ActionResult, error) {
	output, result, err := client.verifyDockerIdentity(ctx)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"version":     output,
			"duration_ms": result.DurationMS,
		},
		DisplayText: truncateString(result.Stdout, 4000),
	}, nil
}

func (client *dockerClient) verifyDockerIdentity(ctx context.Context) (map[string]any, connectors.CommandRunResult, error) {
	result, err := client.run(ctx, client.command+" version --format '{{json .}}'", 15)
	if err != nil {
		return nil, result, err
	}
	if result.ExitCode != 0 {
		return nil, result, dockerCommandError("docker version", result)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &output); err != nil {
		return nil, result, fmt.Errorf("%w: probe returned invalid JSON: %v", errInvalidIdentity, err)
	}
	if !dockerVersionComponentValid(output["Client"]) || !dockerVersionComponentValid(output["Server"]) {
		return nil, result, fmt.Errorf("%w: probe did not return Docker client and server versions", errInvalidIdentity)
	}
	return output, result, nil
}

func dockerVersionComponentValid(value any) bool {
	component, ok := value.(map[string]any)
	if !ok {
		return false
	}
	version, ok := component["Version"].(string)
	return ok && strings.TrimSpace(version) != ""
}

func executeListContainers(ctx context.Context, client *dockerClient, input map[string]any) (connectors.ActionResult, error) {
	containers, err := client.listContainers(ctx, boolValue(input, "all", true))
	if err != nil {
		return connectors.ActionResult{}, err
	}
	sort.SliceStable(containers, func(i, j int) bool {
		return strings.ToLower(containers[i].Name) < strings.ToLower(containers[j].Name)
	})
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"containers": containers,
			"count":      len(containers),
			"scope_mode": client.scope.mode,
		},
		DisplayText: containersDisplay(containers),
	}, nil
}

func executeListImages(ctx context.Context, client *dockerClient) (connectors.ActionResult, error) {
	images, err := client.listImages(ctx)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"images":     images,
			"count":      len(images),
			"scope_mode": client.scope.mode,
		},
		DisplayText: imagesDisplay(images),
	}, nil
}

func executeListNetworks(ctx context.Context, client *dockerClient) (connectors.ActionResult, error) {
	networks, err := client.listNetworks(ctx)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"networks":   networks,
			"count":      len(networks),
			"scope_mode": client.scope.mode,
		},
		DisplayText: networksDisplay(networks),
	}, nil
}

func executeListVolumes(ctx context.Context, client *dockerClient) (connectors.ActionResult, error) {
	volumes, err := client.listVolumes(ctx)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"volumes":    volumes,
			"count":      len(volumes),
			"scope_mode": client.scope.mode,
		},
		DisplayText: volumesDisplay(volumes),
	}, nil
}

func executeInspectContainer(ctx context.Context, client *dockerClient, input map[string]any) (connectors.ActionResult, error) {
	container, err := client.resolveContainer(ctx, stringValue(input, "container"))
	if err != nil {
		return connectors.ActionResult{}, err
	}
	result, err := client.run(ctx, client.command+" inspect -- "+shellQuote(container.Ref()), 20)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if result.ExitCode != 0 {
		return connectors.ActionResult{}, dockerCommandError("docker inspect", result)
	}
	if len(result.Stdout) > maxInspectBytes {
		return connectors.ActionResult{}, fmt.Errorf("docker inspect output is larger than %d bytes", maxInspectBytes)
	}
	raw := result.Stdout
	var inspect []map[string]any
	if err := json.Unmarshal([]byte(raw), &inspect); err != nil {
		return connectors.ActionResult{}, fmt.Errorf("parse docker inspect output: %w", err)
	}
	redacted := redactInspect(inspect)
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"container": container,
			"inspect":   redacted,
		},
		DisplayText: fmt.Sprintf("Inspected Docker container %s.", container.Name),
	}, nil
}

func executeContainerLogs(ctx context.Context, client *dockerClient, input map[string]any) (connectors.ActionResult, error) {
	container, err := client.resolveContainer(ctx, stringValue(input, "container"))
	if err != nil {
		return connectors.ActionResult{}, err
	}
	tail := normalizeInt(input, "tail", defaultLogTail, 1, maxLogTail)
	result, err := client.run(ctx, fmt.Sprintf("%s logs --tail %d --timestamps -- %s 2>&1", client.command, tail, shellQuote(container.Ref())), 30)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if result.ExitCode != 0 {
		return connectors.ActionResult{}, dockerCommandError("docker logs", result)
	}
	logs := truncateString(result.Stdout, maxLogBytes)
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"container":   container,
			"tail":        tail,
			"logs":        logs,
			"duration_ms": result.DurationMS,
		},
		DisplayText: logs,
	}, nil
}

func executeContainerExec(ctx context.Context, client *dockerClient, input map[string]any) (connectors.ActionResult, error) {
	container, err := client.resolveContainer(ctx, stringValue(input, "container"))
	if err != nil {
		return connectors.ActionResult{}, err
	}
	commandText, err := normalizeExecCommandInput(input)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	timeout := normalizeInt(input, "timeout_seconds", 30, 1, 600)
	var options []string
	if user := normalizeDockerOptionInput(input, "user"); user != "" {
		options = append(options, "--user", shellQuote(user))
	}
	if workdir := normalizeDockerOptionInput(input, "workdir"); workdir != "" {
		options = append(options, "--workdir", shellQuote(workdir))
	}
	optionText := ""
	if len(options) > 0 {
		optionText = strings.Join(options, " ") + " "
	}
	result, err := client.run(ctx, fmt.Sprintf("%s exec %s-- %s sh -lc %s 2>&1", client.command, optionText, shellQuote(container.Ref()), shellQuote(commandText)), timeout+5)
	if err != nil {
		return connectors.ActionResult{}, dockerMutationTransportError("docker exec", result, err)
	}
	outputText := truncateString(result.Stdout, maxExecOutputBytes)
	status := connectors.ResultCompleted
	errorText := ""
	if result.ExitCode != 0 {
		status = connectors.ResultFailed
		errorText = fmt.Sprintf("docker exec exited with code %d", result.ExitCode)
		if strings.TrimSpace(result.Stderr) != "" {
			errorText += ": " + truncateString(strings.TrimSpace(result.Stderr), 1000)
		}
	}
	return connectors.ActionResult{
		Status: status,
		Output: map[string]any{
			"container":       container,
			"command":         commandText,
			"exit_code":       result.ExitCode,
			"output":          outputText,
			"timeout_seconds": timeout,
			"user":            normalizeDockerOptionInput(input, "user"),
			"workdir":         normalizeDockerOptionInput(input, "workdir"),
			"duration_ms":     result.DurationMS,
		},
		DisplayText: outputText,
		Error:       errorText,
	}, nil
}

func executeContainerLifecycle(ctx context.Context, client *dockerClient, input map[string]any, operation string) (connectors.ActionResult, error) {
	container, err := client.resolveContainer(ctx, stringValue(input, "container"))
	if err != nil {
		return connectors.ActionResult{}, err
	}
	timeout := normalizeInt(input, "timeout_seconds", 10, 1, 120)
	command := fmt.Sprintf("%s %s", client.command, operation)
	if operation == "stop" || operation == "restart" {
		command = fmt.Sprintf("%s %s --time %d", client.command, operation, timeout)
	}
	result, err := client.run(ctx, command+" -- "+shellQuote(container.Ref())+" 2>&1", timeout+20)
	if err != nil {
		return connectors.ActionResult{}, dockerMutationTransportError("docker "+operation, result, err)
	}
	if result.ExitCode != 0 {
		return connectors.ActionResult{}, dockerCommandError("docker "+operation, result)
	}
	output := map[string]any{
		"container":   container,
		"operation":   operation,
		"response":    strings.TrimSpace(result.Stdout),
		"duration_ms": result.DurationMS,
	}
	if refreshed, err := client.resolveContainer(ctx, container.Ref()); err == nil {
		output["container"] = refreshed
	} else {
		output["refresh_error"] = err.Error()
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: fmt.Sprintf("Docker container %s %s completed.", container.Name, operation),
	}, nil
}

func dockerMutationTransportError(operation string, result connectors.CommandRunResult, err error) error {
	if err == nil || !result.DispatchStarted {
		return err
	}
	return connectors.ClassifyOutcomeUnknown(
		"command_transport",
		nil,
		fmt.Errorf("%s outcome is unknown after dispatch: %w", operation, err),
	)
}
