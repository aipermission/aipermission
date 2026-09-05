package dockerconnector

import (
	"context"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

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
			Default:     defaultDockerCommand,
			Description: "Use docker or an absolute Docker-compatible wrapper path on the remote host. Shell arguments are not accepted.",
		},
	}}
}

func (Connector) ValidateTargetConfig(config map[string]any) error {
	_, err := DockerCommand(connectors.TargetView{Config: config})
	return err
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
			"Use list_images, list_networks, and list_volumes for read-only Docker host inventory.",
			"Use container_logs with a bounded tail value for recent logs.",
			"Use inspect_container for redacted Docker metadata. Environment variables are masked.",
			"Use container_exec for bounded non-interactive commands inside one scoped container.",
			"Use start_container, stop_container, or restart_container only when the operator intends a container lifecycle change.",
		},
		Warnings: []string{
			"Docker actions run through a selected transport profile. The Docker connector does not expose arbitrary host-level docker commands, prune, rm, or shell access.",
			"container_exec can change data inside the container. Prefer prompt mode unless the workflow is trusted.",
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
			Name:        ActionListImages,
			Label:       "List images",
			Description: "List Docker images visible to this credential profile scope.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{},
			OutputHint:  connectors.OutputHint{Format: "json", MaxRows: 500},
		},
		{
			Name:        ActionListNetworks,
			Label:       "List networks",
			Description: "List Docker networks. Selected scopes only return networks attached to scoped containers.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{},
			OutputHint:  connectors.OutputHint{Format: "json", MaxRows: 500},
		},
		{
			Name:        ActionListVolumes,
			Label:       "List volumes",
			Description: "List Docker volumes. Selected scopes only return volumes mounted by scoped containers.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{},
			OutputHint:  connectors.OutputHint{Format: "json", MaxRows: 500},
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
				{Name: "tail", Label: "Tail lines", Type: connectors.FieldInteger, Default: defaultLogTail},
			}},
			OutputHint: connectors.OutputHint{Format: "text", MaxBytes: maxLogBytes},
		},
		{
			Name:        ActionContainerExec,
			Label:       "Exec in container",
			Description: "Run one bounded non-interactive shell command inside one scoped Docker container.",
			Category:    "execution",
			Risk:        connectors.RiskDestructive,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "container", Label: "Container", Type: connectors.FieldString, Required: true},
				{Name: "command", Label: "Command", Type: connectors.FieldMultiline, Required: true},
				{Name: "timeout_seconds", Label: "Timeout seconds", Type: connectors.FieldInteger, Default: 30},
				{Name: "user", Label: "User", Type: connectors.FieldString, Description: "Optional container user."},
				{Name: "workdir", Label: "Working directory", Type: connectors.FieldString, Description: "Optional container working directory."},
			}},
			OutputHint: connectors.OutputHint{Format: "text", MaxBytes: maxExecOutputBytes},
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
				{Name: "timeout_seconds", Label: "Timeout seconds", Type: connectors.FieldInteger, Default: 10},
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
				{Name: "timeout_seconds", Label: "Timeout seconds", Type: connectors.FieldInteger, Default: 10},
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
	case ActionListImages:
		title = "List Docker images"
		summary = "List Docker images visible to this credential profile scope."
	case ActionListNetworks:
		title = "List Docker networks"
		summary = "List Docker networks visible to this credential profile scope."
	case ActionListVolumes:
		title = "List Docker volumes"
		summary = "List Docker volumes visible to this credential profile scope."
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
	case ActionContainerExec:
		risk = connectors.RiskDestructive
		container, err := normalizeContainerInput(input)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		command, err := normalizeExecCommandInput(input)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		input["container"] = container
		input["command"] = command
		input["timeout_seconds"] = normalizeInt(input, "timeout_seconds", 30, 1, 600)
		input["user"] = normalizeDockerOptionInput(input, "user")
		input["workdir"] = normalizeDockerOptionInput(input, "workdir")
		title = "Exec inside Docker container"
		summary = fmt.Sprintf("%s: %s", container, firstLine(command))
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
		Dependencies:  connectors.CommandTransportDependencies(req.Target),
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
