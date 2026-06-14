// Package sshconnector defines the SSH connector contract.
package sshconnector

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Kind    = "ssh"
	Label   = "SSH"
	Version = "0.2"

	ActionExec                  = "exec"
	ActionReadConsole           = "read_console"
	ActionRestartConsoleSession = "restart_console_session"
	ActionBrowseRemoteFiles     = "browse_remote_files"
	ActionStartFileDownload     = "start_file_download"

	RuntimeServiceName = "ssh"

	defaultConsoleTailBytes = 20000
	maxConsoleTailBytes     = 100000
)

var (
	ErrUnsupportedAction  = errors.New("unsupported ssh connector action")
	ErrRuntimeUnavailable = errors.New("ssh connector runtime is unavailable")
)

type RuntimeExecutor interface {
	ExecuteSSHAction(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error)
}

// Connector describes the SSH feature set as a connector-shaped target.
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
			Name:        "host",
			Label:       "Host",
			Type:        connectors.FieldString,
			Required:    true,
			Description: "DNS name or IP address of the SSH target.",
		},
		{
			Name:        "port",
			Label:       "Port",
			Type:        connectors.FieldNumber,
			Required:    true,
			Default:     22,
			Description: "SSH port.",
		},
		{
			Name:        "description",
			Label:       "Description",
			Type:        connectors.FieldMultiline,
			Description: "Non-secret operator notes. This may be visible to AI clients.",
		},
		{
			Name:        "startup_input_after_connect",
			Label:       "Startup input after connect",
			Type:        connectors.FieldString,
			Description: "Optional text sent after PTY connect for menu-based devices such as NAS appliances.",
		},
		{
			Name:        "force_shell_command",
			Label:       "Force shell command",
			Type:        connectors.FieldString,
			Description: "Optional shell command used for targets that need an explicit shell after login.",
		},
	}}
}

func (Connector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{
		{
			Kind:        "private_key",
			Label:       "SSH private key",
			Description: "Gateway-managed or imported SSH private key referenced by this target profile.",
			Schema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "username",
					Label:       "Username",
					Type:        connectors.FieldString,
					Required:    true,
					Description: "Remote SSH username for this credential profile.",
				},
				{
					Name:        "ssh_key_id",
					Label:       "Gateway key id",
					Type:        connectors.FieldNumber,
					Required:    true,
					Description: "Local encrypted SSH key material selected from Credentials.",
				},
				{
					Name:        "key_name",
					Label:       "Key name",
					Type:        connectors.FieldString,
					Description: "Public local key label copied for display.",
				},
				{
					Name:        "key_type",
					Label:       "Key type",
					Type:        connectors.FieldString,
					Description: "Public local key type copied for display.",
				},
				{
					Name:        "fingerprint",
					Label:       "Fingerprint",
					Type:        connectors.FieldString,
					Description: "Public SSH key fingerprint copied for display.",
				},
			}},
		},
	}
}

func (Connector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	title := "SSH target"
	if target.Name != "" {
		title = "SSH target: " + target.Name
	}
	return connectors.ConnectorHelp{
		Title:       title,
		Summary:     "Run bounded non-interactive shell commands through AIPermission's local SSH gateway.",
		Connector:   Label,
		ConnectorID: Kind,
		Usage: []string{
			"Use exec for shell commands. Include a short reason so the operator can approve or audit the action.",
			"Use read_console only for always-run targets when you need live persistent console output.",
			"Use restart_console_session when a persistent console appears stuck before sending more commands.",
			"Use browse_remote_files before file transfers when the remote path is uncertain.",
			"Use start_file_download for remote-to-local transfer queues.",
			"Prefer bounded output: tail -n, journalctl --no-pager -n, docker logs --tail, or redirect full output to a temp file.",
		},
		Warnings: []string{
			"SSH commands execute on the target shell after token permission and approval checks.",
			"Avoid printing secrets. Redaction is best-effort and cannot make secret output safe.",
			"Connector target visibility is not a live SSH health check; execution errors are the reachability signal.",
		},
	}, nil
}

func (Connector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{
		{
			Name:        ActionExec,
			Label:       "Run command",
			Description: "Run a non-interactive command in the target's persistent SSH console.",
			Category:    "command",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "command",
					Label:       "Command",
					Type:        connectors.FieldMultiline,
					Required:    true,
					Description: "Shell command to run on the target.",
				},
			}},
			OutputHint: connectors.OutputHint{Format: "terminal", MaxBytes: 100000},
		},
		{
			Name:        ActionReadConsole,
			Label:       "Read console",
			Description: "Read the latest persistent SSH console transcript.",
			Category:    "console",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "tail_bytes",
					Label:       "Tail bytes",
					Type:        connectors.FieldNumber,
					Default:     defaultConsoleTailBytes,
					Description: "Maximum trailing transcript bytes to return.",
				},
			}},
			OutputHint: connectors.OutputHint{Format: "terminal", MaxBytes: maxConsoleTailBytes},
		},
		{
			Name:        ActionRestartConsoleSession,
			Label:       "Restart console session",
			Description: "Close the gateway-owned persistent SSH console session so the next action reconnects.",
			Category:    "console",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{},
			OutputHint:  connectors.OutputHint{Format: "json"},
		},
		{
			Name:        ActionBrowseRemoteFiles,
			Label:       "Browse remote files",
			Description: "List files in a remote directory before choosing transfer paths.",
			Category:    "files",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "path",
					Label:       "Remote path",
					Type:        connectors.FieldString,
					Default:     "~",
					Description: "Remote directory to list.",
				},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: 500},
		},
		{
			Name:        ActionStartFileDownload,
			Label:       "Start file download",
			Description: "Create a remote-to-local file transfer queue.",
			Category:    "files",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "remote_paths",
					Label:       "Remote paths",
					Type:        connectors.FieldJSON,
					Required:    true,
					Description: "Array of absolute remote file paths to download.",
				},
				{
					Name:        "archive_name",
					Label:       "Archive name",
					Type:        connectors.FieldString,
					Description: "Optional archive filename for multi-file downloads.",
				},
			}},
			OutputHint: connectors.OutputHint{Format: "json"},
		},
	}, nil
}

func (Connector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	base := connectors.PreparedAction{
		ConnectorKind: Kind,
		TargetRef:     req.Target.Ref,
		ProfileID:     req.Profile.ID,
		ActionName:    req.ActionName,
		ContextMaterial: map[string]any{
			"connector_kind": Kind,
			"target_ref":     req.Target.Ref,
			"profile_id":     req.Profile.ID,
			"action_name":    req.ActionName,
		},
	}

	switch req.ActionName {
	case ActionExec:
		command := strings.TrimSpace(stringInput(req.Input, "command"))
		if command == "" {
			return connectors.PreparedAction{}, fmt.Errorf("%s command is required", ActionExec)
		}
		base.Risk = connectors.RiskWrite
		base.Title = "Run SSH command"
		base.Summary = targetSummary(req.Target, "Run a shell command")
		base.Preview = map[string]any{
			"command": command,
			"reason":  strings.TrimSpace(req.Reason),
		}
		base.Payload = map[string]any{
			"command": command,
			"reason":  strings.TrimSpace(req.Reason),
		}
		base.ContextMaterial["command"] = command
		base.ContextMaterial["reason"] = strings.TrimSpace(req.Reason)
		return base, nil
	case ActionReadConsole:
		tail := intInput(req.Input, "tail_bytes", defaultConsoleTailBytes)
		if tail < 1 {
			tail = defaultConsoleTailBytes
		}
		if tail > maxConsoleTailBytes {
			tail = maxConsoleTailBytes
		}
		base.Risk = connectors.RiskRead
		base.Title = "Read SSH console"
		base.Summary = targetSummary(req.Target, "Read the latest console transcript")
		base.Preview = map[string]any{"tail_bytes": tail}
		base.Payload = map[string]any{"tail_bytes": tail}
		base.ContextMaterial["tail_bytes"] = tail
		return base, nil
	case ActionRestartConsoleSession:
		base.Risk = connectors.RiskWrite
		base.Title = "Restart SSH console session"
		base.Summary = targetSummary(req.Target, "Restart the persistent console session")
		base.Preview = map[string]any{}
		base.Payload = map[string]any{}
		return base, nil
	case ActionBrowseRemoteFiles:
		remotePath := strings.TrimSpace(stringInput(req.Input, "path"))
		if remotePath == "" {
			remotePath = "~"
		}
		base.Risk = connectors.RiskRead
		base.Title = "Browse remote files"
		base.Summary = targetSummary(req.Target, "Browse a remote directory")
		base.Preview = map[string]any{"path": remotePath}
		base.Payload = map[string]any{"path": remotePath}
		base.ContextMaterial["path"] = remotePath
		return base, nil
	case ActionStartFileDownload:
		remotePaths := stringSliceInput(req.Input, "remote_paths")
		if len(remotePaths) == 0 {
			return connectors.PreparedAction{}, fmt.Errorf("%s remote_paths is required", ActionStartFileDownload)
		}
		archiveName := strings.TrimSpace(stringInput(req.Input, "archive_name"))
		base.Risk = connectors.RiskRead
		base.Title = "Start SSH file download"
		base.Summary = targetSummary(req.Target, "Start a remote-to-local transfer queue")
		base.Preview = map[string]any{
			"remote_paths": remotePaths,
			"archive_name": archiveName,
			"items":        len(remotePaths),
		}
		base.Payload = map[string]any{
			"remote_paths": remotePaths,
			"archive_name": archiveName,
		}
		base.ContextMaterial["remote_paths"] = remotePaths
		base.ContextMaterial["archive_name"] = archiveName
		return base, nil
	default:
		return connectors.PreparedAction{}, fmt.Errorf("%w: %s", ErrUnsupportedAction, req.ActionName)
	}
}

func (Connector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	executor, ok := runtime.Capability(RuntimeServiceName).(RuntimeExecutor)
	if !ok || executor == nil {
		return connectors.ActionResult{}, ErrRuntimeUnavailable
	}
	return executor.ExecuteSSHAction(ctx, runtime, action)
}

func targetSummary(target connectors.TargetView, action string) string {
	if target.Name == "" {
		return action + " on SSH target."
	}
	return action + " on " + target.Name + "."
}

func stringInput(input map[string]any, name string) string {
	if input == nil {
		return ""
	}
	value, ok := input[name]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func intInput(input map[string]any, name string, fallback int) int {
	if input == nil {
		return fallback
	}
	value, ok := input[name]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return fallback
	}
}

func boolInput(input map[string]any, name string) bool {
	if input == nil {
		return false
	}
	value, ok := input[name]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func stringSliceInput(input map[string]any, name string) []string {
	if input == nil {
		return nil
	}
	value, ok := input[name]
	if !ok || value == nil {
		return nil
	}
	var raw []string
	switch typed := value.(type) {
	case []string:
		raw = typed
	case []any:
		for _, item := range typed {
			raw = append(raw, strings.TrimSpace(fmt.Sprint(item)))
		}
	case string:
		raw = []string{typed}
	default:
		raw = []string{fmt.Sprint(typed)}
	}
	clean := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item != "" {
			clean = append(clean, item)
		}
	}
	return clean
}
