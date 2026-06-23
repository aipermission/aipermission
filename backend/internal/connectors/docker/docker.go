// Package dockerconnector defines the Docker connector contract.
package dockerconnector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Kind    = "docker"
	Label   = "Docker"
	Version = "0.2"

	ActionVersion          = "docker_version"
	ActionListContainers   = "list_containers"
	ActionInspectContainer = "inspect_container"
	ActionContainerLogs    = "container_logs"
	ActionStartContainer   = "start_container"
	ActionStopContainer    = "stop_container"
	ActionRestartContainer = "restart_container"

	defaultLogTail     = 200
	maxLogTail         = 2000
	maxLogBytes        = 256 << 10
	maxInspectBytes    = 512 << 10
	maxDockerReasonLen = 2000
)

var (
	ErrUnsupportedAction = errors.New("unsupported docker connector action")
	ErrMissingTransport  = errors.New("docker connector command transport is unavailable")
	ErrInvalidConfig     = errors.New("docker connector target config is invalid")
	ErrScopeDenied       = errors.New("docker container is outside this credential profile scope")
)

type Connector struct{}

func New() Connector {
	return Connector{}
}

func (Connector) Kind() string {
	return Kind
}

func (Connector) Label() string {
	return Label
}

func (Connector) Version() string {
	return Version
}

func (Connector) TargetSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{
		{
			Name:        "connection_mode",
			Label:       "Connection mode",
			Type:        connectors.FieldSelect,
			Required:    true,
			Default:     "over_ssh",
			Description: "Run bounded Docker CLI templates through an SSH connector profile.",
			Options: []connectors.FieldOption{
				{Value: "over_ssh", Label: "Over SSH"},
			},
		},
		{
			Name:        "transport_target_ref",
			Label:       "SSH transport profile",
			Type:        connectors.FieldString,
			Required:    true,
			Description: "SSH connector target/profile ref used to run docker commands.",
		},
		{
			Name:        "docker_command",
			Label:       "Docker command",
			Type:        connectors.FieldString,
			Default:     "docker",
			Description: "Docker CLI command on the remote host. Keep this as docker unless the host uses a wrapper path.",
		},
	}}
}

func (Connector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{
		{
			Kind:        "container_scope",
			Label:       "Container scope",
			Description: "Restrict this profile to all containers or to selected container names/IDs/patterns.",
			Schema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "scope_mode",
					Label:       "Scope",
					Type:        connectors.FieldSelect,
					Required:    true,
					Default:     "all",
					Description: "Use selected when this token should only see and operate on specific containers.",
					Options: []connectors.FieldOption{
						{Value: "all", Label: "All containers"},
						{Value: "selected", Label: "Selected containers"},
					},
				},
				{
					Name:        "allowed_containers",
					Label:       "Allowed containers",
					Type:        connectors.FieldMultiline,
					Description: "One container name, full ID, or ID prefix per line.",
				},
				{
					Name:        "allowed_patterns",
					Label:       "Allowed name patterns",
					Type:        connectors.FieldMultiline,
					Description: "Optional shell-style name patterns such as app-* or project_web_*.",
				},
			}},
		},
	}
}

func (Connector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	title := "Docker target"
	if strings.TrimSpace(target.Name) != "" {
		title = "Docker target: " + target.Name
	}
	return connectors.ConnectorHelp{
		Title:       title,
		Summary:     "Inspect and control Docker containers through bounded Docker CLI templates and AIPermission approval rules.",
		Connector:   Label,
		ConnectorID: Kind,
		Usage: []string{
			"Use list_containers before targeting a container by name or ID.",
			"Use container_logs with a bounded tail value for recent logs.",
			"Use inspect_container for redacted Docker metadata. Environment variables are masked.",
			"Use start_container, stop_container, or restart_container only when the operator intends a container lifecycle change.",
		},
		Warnings: []string{
			"Docker actions run through a selected transport profile. The Docker connector does not expose arbitrary docker exec, prune, rm, or shell access.",
			"Credential profile scope can restrict AI access to one container or a small allowed set.",
			"Container logs may contain secrets. Redaction is best-effort; avoid requesting sensitive logs unless approved.",
		},
	}, nil
}

func (Connector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{
		{
			Name:        ActionVersion,
			Label:       "Docker version",
			Description: "Read Docker client/server version metadata.",
			Category:    "metadata",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{},
			OutputHint:  connectors.OutputHint{Format: "json", MaxBytes: 64 << 10},
		},
		{
			Name:        ActionListContainers,
			Label:       "List containers",
			Description: "List containers visible to this credential profile scope.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "all", Label: "Include stopped", Type: connectors.FieldBoolean, Default: true},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: 500},
		},
		{
			Name:        ActionInspectContainer,
			Label:       "Inspect container",
			Description: "Read redacted Docker inspect metadata for one scoped container.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "container", Label: "Container", Type: connectors.FieldString, Required: true},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: maxInspectBytes},
		},
		{
			Name:        ActionContainerLogs,
			Label:       "Container logs",
			Description: "Read a bounded tail of one scoped container's logs.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "container", Label: "Container", Type: connectors.FieldString, Required: true},
				{Name: "tail", Label: "Tail lines", Type: connectors.FieldNumber, Default: defaultLogTail},
			}},
			OutputHint: connectors.OutputHint{Format: "text", MaxBytes: maxLogBytes},
		},
		{
			Name:        ActionStartContainer,
			Label:       "Start container",
			Description: "Start one scoped Docker container.",
			Category:    "lifecycle",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "container", Label: "Container", Type: connectors.FieldString, Required: true},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
		{
			Name:        ActionStopContainer,
			Label:       "Stop container",
			Description: "Stop one scoped Docker container.",
			Category:    "lifecycle",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "container", Label: "Container", Type: connectors.FieldString, Required: true},
				{Name: "timeout_seconds", Label: "Timeout seconds", Type: connectors.FieldNumber, Default: 10},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
		{
			Name:        ActionRestartContainer,
			Label:       "Restart container",
			Description: "Restart one scoped Docker container.",
			Category:    "lifecycle",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "container", Label: "Container", Type: connectors.FieldString, Required: true},
				{Name: "timeout_seconds", Label: "Timeout seconds", Type: connectors.FieldNumber, Default: 10},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
	}, nil
}

func (Connector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	input := copyMap(req.Input)
	if len(req.Reason) > maxDockerReasonLen {
		return connectors.PreparedAction{}, fmt.Errorf("reason is too large")
	}
	risk := connectors.RiskRead
	title := ""
	summary := ""
	switch req.ActionName {
	case ActionVersion:
		title = "Read Docker version"
		summary = "Read Docker client/server version metadata."
	case ActionListContainers:
		input["all"] = boolValue(input, "all", true)
		title = "List Docker containers"
		summary = "List containers visible to this credential profile scope."
	case ActionInspectContainer:
		container, err := normalizeContainerInput(input)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		input["container"] = container
		title = "Inspect Docker container"
		summary = container
	case ActionContainerLogs:
		container, err := normalizeContainerInput(input)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		input["container"] = container
		input["tail"] = normalizeInt(input, "tail", defaultLogTail, 1, maxLogTail)
		title = "Read Docker container logs"
		summary = fmt.Sprintf("%s tail=%d", container, input["tail"])
	case ActionStartContainer:
		risk = connectors.RiskWrite
		container, err := normalizeContainerInput(input)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		input["container"] = container
		title = "Start Docker container"
		summary = container
	case ActionStopContainer:
		risk = connectors.RiskWrite
		container, err := normalizeContainerInput(input)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		input["container"] = container
		input["timeout_seconds"] = normalizeInt(input, "timeout_seconds", 10, 1, 120)
		title = "Stop Docker container"
		summary = fmt.Sprintf("%s timeout=%ss", container, fmt.Sprint(input["timeout_seconds"]))
	case ActionRestartContainer:
		risk = connectors.RiskWrite
		container, err := normalizeContainerInput(input)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		input["container"] = container
		input["timeout_seconds"] = normalizeInt(input, "timeout_seconds", 10, 1, 120)
		title = "Restart Docker container"
		summary = fmt.Sprintf("%s timeout=%ss", container, fmt.Sprint(input["timeout_seconds"]))
	default:
		return connectors.PreparedAction{}, ErrUnsupportedAction
	}
	return connectors.PreparedAction{
		ConnectorKind: Kind,
		TargetRef:     req.Target.Ref,
		ProfileID:     req.Profile.ID,
		ActionName:    req.ActionName,
		Risk:          risk,
		Title:         title,
		Summary:       summary,
		Preview:       input,
		Payload:       input,
		ContextMaterial: map[string]any{
			"target":          req.Target.Name,
			"profile":         req.Profile.Label,
			"connection_mode": connectionMode(req.Target),
			"scope_mode":      scopeMode(req.Profile),
		},
	}, nil
}

func (Connector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	client, err := newDockerClient(runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	switch action.ActionName {
	case ActionVersion:
		return executeVersion(ctx, client)
	case ActionListContainers:
		return executeListContainers(ctx, client, action.Payload)
	case ActionInspectContainer:
		return executeInspectContainer(ctx, client, action.Payload)
	case ActionContainerLogs:
		return executeContainerLogs(ctx, client, action.Payload)
	case ActionStartContainer:
		return executeContainerLifecycle(ctx, client, action.Payload, "start")
	case ActionStopContainer:
		return executeContainerLifecycle(ctx, client, action.Payload, "stop")
	case ActionRestartContainer:
		return executeContainerLifecycle(ctx, client, action.Payload, "restart")
	default:
		return connectors.ActionResult{}, ErrUnsupportedAction
	}
}

func (Connector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	client, err := newDockerClient(runtime)
	if err != nil {
		return connectors.TestResult{Status: connectors.TestUnknownError, Message: err.Error()}, nil
	}
	result, err := client.run(ctx, "docker version --format '{{json .}}'", 15)
	if err != nil {
		return connectors.TestResult{Status: connectors.TestFailedNetwork, Message: err.Error()}, nil
	}
	if result.ExitCode != 0 {
		return connectors.TestResult{Status: connectors.TestFailedPermission, Message: dockerCommandError("docker version", result).Error()}, nil
	}
	return connectors.TestResult{
		Status:  connectors.TestOK,
		Message: "Docker connection ok.",
		Details: map[string]any{
			"duration_ms": result.DurationMS,
			"mode":        connectionMode(runtime.Target),
		},
	}, nil
}

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
	command := dockerCommand(runtime.Target)
	if command == "" {
		return nil, fmt.Errorf("%w: docker_command is required", ErrInvalidConfig)
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
		Mode:               connectionMode(client.runtime.Target),
		TransportTargetRef: strings.TrimSpace(stringValue(client.runtime.Target.Config, "transport_target_ref")),
		Command:            command,
		TimeoutSeconds:     timeoutSeconds,
	})
}

func executeVersion(ctx context.Context, client *dockerClient) (connectors.ActionResult, error) {
	result, err := client.run(ctx, client.command+" version --format '{{json .}}'", 15)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if result.ExitCode != 0 {
		return connectors.ActionResult{}, dockerCommandError("docker version", result)
	}
	var output any
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		output = map[string]any{"raw": truncateString(result.Stdout, maxLogBytes)}
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
		return connectors.ActionResult{}, err
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

type DockerContainer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Command string `json:"command,omitempty"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Ports   string `json:"ports,omitempty"`
	Labels  string `json:"labels,omitempty"`
}

func (container DockerContainer) Ref() string {
	if strings.TrimSpace(container.Name) != "" {
		return container.Name
	}
	return container.ID
}

func (client *dockerClient) listContainers(ctx context.Context, includeStopped bool) ([]DockerContainer, error) {
	args := "ps --no-trunc --format '{{json .}}'"
	if includeStopped {
		args = "ps -a --no-trunc --format '{{json .}}'"
	}
	result, err := client.run(ctx, client.command+" "+args, 20)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, dockerCommandError("docker ps", result)
	}
	containers, err := parseDockerPS(result.Stdout)
	if err != nil {
		return nil, err
	}
	filtered := containers[:0]
	for _, container := range containers {
		if client.scope.allows(container) {
			filtered = append(filtered, container)
		}
	}
	return filtered, nil
}

func (client *dockerClient) resolveContainer(ctx context.Context, requested string) (DockerContainer, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return DockerContainer{}, fmt.Errorf("container is required")
	}
	containers, err := client.listContainers(ctx, true)
	if err != nil {
		return DockerContainer{}, err
	}
	for _, container := range containers {
		if container.Name == requested || container.ID == requested || strings.HasPrefix(container.ID, requested) {
			return container, nil
		}
	}
	return DockerContainer{}, fmt.Errorf("%w: %s", ErrScopeDenied, requested)
}

func parseDockerPS(data string) ([]DockerContainer, error) {
	var containers []DockerContainer
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parse docker ps row: %w", err)
		}
		container := DockerContainer{
			ID:      strings.TrimSpace(stringValue(row, "ID")),
			Name:    strings.TrimSpace(stringValue(row, "Names")),
			Image:   strings.TrimSpace(stringValue(row, "Image")),
			Command: strings.TrimSpace(stringValue(row, "Command")),
			State:   strings.TrimSpace(stringValue(row, "State")),
			Status:  strings.TrimSpace(stringValue(row, "Status")),
			Ports:   strings.TrimSpace(stringValue(row, "Ports")),
			Labels:  strings.TrimSpace(stringValue(row, "Labels")),
		}
		if container.ID == "" && container.Name == "" {
			continue
		}
		containers = append(containers, container)
	}
	return containers, nil
}

type dockerScope struct {
	mode     string
	exact    []string
	patterns []string
}

func dockerScopeFromProfile(profile connectors.CredentialProfileView) dockerScope {
	mode := scopeMode(profile)
	return dockerScope{
		mode:     mode,
		exact:    splitLines(stringValue(profile.Public, "allowed_containers")),
		patterns: splitLines(stringValue(profile.Public, "allowed_patterns")),
	}
}

func (scope dockerScope) allows(container DockerContainer) bool {
	if scope.mode != "selected" {
		return true
	}
	candidates := []string{container.ID, container.Name}
	if len(container.ID) >= 12 {
		candidates = append(candidates, container.ID[:12])
	}
	for _, allowed := range scope.exact {
		for _, candidate := range candidates {
			if allowed == candidate || strings.HasPrefix(container.ID, allowed) {
				return true
			}
		}
	}
	for _, pattern := range scope.patterns {
		if ok, _ := path.Match(pattern, container.Name); ok {
			return true
		}
	}
	return false
}

func scopeMode(profile connectors.CredentialProfileView) string {
	mode := strings.TrimSpace(stringValue(profile.Public, "scope_mode"))
	if mode == "selected" {
		return "selected"
	}
	return "all"
}

func connectionMode(target connectors.TargetView) string {
	mode := strings.TrimSpace(stringValue(target.Config, "connection_mode"))
	if mode == "" {
		return "over_ssh"
	}
	return mode
}

func dockerCommand(target connectors.TargetView) string {
	value := strings.TrimSpace(stringValue(target.Config, "docker_command"))
	if value == "" {
		value = "docker"
	}
	if strings.ContainsAny(value, "\n\r\t;&|`$<>") {
		return ""
	}
	return value
}

func normalizeContainerInput(input map[string]any) (string, error) {
	container := strings.TrimSpace(stringValue(input, "container"))
	if container == "" {
		return "", fmt.Errorf("container is required")
	}
	if strings.ContainsAny(container, "\x00\n\r") {
		return "", fmt.Errorf("container contains unsupported characters")
	}
	return container, nil
}

func dockerCommandError(command string, result connectors.CommandRunResult) error {
	message := strings.TrimSpace(result.Stderr)
	if message == "" {
		message = strings.TrimSpace(result.Stdout)
	}
	if message == "" {
		message = fmt.Sprintf("%s failed with exit code %d", command, result.ExitCode)
	}
	return fmt.Errorf("%s failed: %s", command, truncateString(message, 4000))
}

func containersDisplay(containers []DockerContainer) string {
	if len(containers) == 0 {
		return "No Docker containers matched this profile scope."
	}
	lines := make([]string, 0, len(containers))
	for _, container := range containers {
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s", container.Name, container.Image, container.State, container.Status))
	}
	return strings.Join(lines, "\n")
}

func redactInspect(items []map[string]any) []map[string]any {
	for _, item := range items {
		if config, ok := item["Config"].(map[string]any); ok {
			if env, ok := config["Env"].([]any); ok {
				redacted := make([]any, 0, len(env))
				for _, value := range env {
					text := fmt.Sprint(value)
					name, _, found := strings.Cut(text, "=")
					if found && strings.TrimSpace(name) != "" {
						redacted = append(redacted, name+"=***")
					} else {
						redacted = append(redacted, "***")
					}
				}
				config["Env"] = redacted
			}
		}
	}
	return items
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func splitLines(value string) []string {
	var out []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func copyMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	switch value := values[key].(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func boolValue(values map[string]any, key string, fallback bool) bool {
	if values == nil {
		return fallback
	}
	switch value := values[key].(type) {
	case bool:
		return value
	case string:
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	case float64:
		return value != 0
	case int:
		return value != 0
	}
	return fallback
}

func normalizeInt(values map[string]any, key string, fallback int, minValue int, maxValue int) int {
	value := intValue(values, key, fallback)
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func intValue(values map[string]any, key string, fallback int) int {
	if values == nil {
		return fallback
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, err := value.Int64()
		if err == nil {
			return int(parsed)
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func truncateString(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "\n...[truncated]"
}
