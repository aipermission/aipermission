// Package sshconnector defines the SSH connector contract without wiring it
// into the current runtime path yet.
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
	Version = "0.1"

	ActionExec                  = "exec"
	ActionReadConsole           = "read_console"
	ActionRestartConsoleSession = "restart_console_session"

	defaultConsoleTailBytes = 20000
	maxConsoleTailBytes     = 100000
)

var (
	ErrUnsupportedAction = errors.New("unsupported ssh connector action")
	ErrExecutionNotWired = errors.New("ssh connector execution is not wired yet")
)

// Connector describes the current SSH feature set as a connector-shaped target.
// Existing MCP/API handlers still own execution until the connector runtime is
// introduced.
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
	}}
}

func (Connector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{
		{
			Kind:        "private_key",
			Label:       "SSH private key",
			Description: "Gateway-managed or imported SSH private key. Passphrases are used only during import and are not stored.",
			Schema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "username",
					Label:       "Username",
					Type:        connectors.FieldString,
					Required:    true,
					Description: "Remote SSH username for this credential profile.",
				},
				{
					Name:        "private_key",
					Label:       "Private key",
					Type:        connectors.FieldMultilineSecret,
					Required:    true,
					Secret:      true,
					Description: "Private key material stored through the encrypted vault layer.",
				},
				{
					Name:        "public_key",
					Label:       "Public key",
					Type:        connectors.FieldMultiline,
					Description: "Public key line used for remote authorized_keys installation.",
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
			"Prefer bounded output: tail -n, journalctl --no-pager -n, docker logs --tail, or redirect full output to a temp file.",
		},
		Warnings: []string{
			"SSH commands execute on the target shell after token permission and approval checks.",
			"Avoid printing secrets. Redaction is best-effort and cannot make secret output safe.",
			"list_servers style target metadata is not a live health check; execution errors are the reachability signal.",
		},
	}, nil
}

func (Connector) GetActionList(context.Context, connectors.TargetView) ([]connectors.ActionDefinition, error) {
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
	default:
		return connectors.PreparedAction{}, fmt.Errorf("%w: %s", ErrUnsupportedAction, req.ActionName)
	}
}

func (Connector) ExecuteAction(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error) {
	return connectors.ActionResult{}, ErrExecutionNotWired
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
