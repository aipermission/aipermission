package redisconnector

import (
	"context"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func (Connector) TargetSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{
		{
			Name:        "server_family",
			Label:       "Server product",
			Type:        connectors.FieldSelect,
			Default:     ServerFamilyRedis,
			Description: "Choose Redis or Valkey. Both use the same bounded RESP connector actions.",
			Options: []connectors.FieldOption{
				{Value: ServerFamilyRedis, Label: "Redis"},
				{Value: ServerFamilyValkey, Label: "Valkey"},
			},
		},
		{
			Name:        "connection_mode",
			Label:       "Connection mode",
			Type:        connectors.FieldSelect,
			Required:    true,
			Default:     "direct",
			Description: "Connect directly from the local gateway, or tunnel through an SSH connector profile.",
			Options: []connectors.FieldOption{
				{Value: "direct", Label: "Direct"},
				{Value: "over_ssh", Label: "Over SSH"},
			},
		},
		{
			Name:        "host",
			Label:       "Host",
			Type:        connectors.FieldString,
			Required:    true,
			Default:     defaultRedisHost,
			Description: "Redis-compatible host as seen by the selected connection mode. For Over SSH this is usually 127.0.0.1 on the remote server.",
		},
		{
			Name:        "port",
			Label:       "Port",
			Type:        connectors.FieldInteger,
			Required:    true,
			Default:     defaultRedisPort,
			Description: "Redis-compatible RESP TCP port.",
		},
		{
			Name:        "database",
			Label:       "Database",
			Type:        connectors.FieldInteger,
			Default:     0,
			Description: "Logical database number.",
		},
		{
			Name:        "tls_mode",
			Label:       "TLS mode",
			Type:        connectors.FieldSelect,
			Required:    true,
			Default:     "auto",
			Description: "Auto verifies TLS for remote direct endpoints and uses plaintext for local or SSH-tunneled endpoints.",
			Options: []connectors.FieldOption{
				{Value: "auto", Label: "Auto"},
				{Value: "disable", Label: "Disable"},
				{Value: "verify_full", Label: "Verify full"},
			},
		},
		{
			Name:        "transport_target_ref",
			Label:       "SSH transport target",
			Type:        connectors.FieldString,
			Description: "Connector target ref used when connection_mode is over_ssh.",
		},
	}}
}

func (Connector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{
		{
			Kind:        "username_password",
			Label:       "Username and password",
			Description: "Redis or Valkey ACL username and password stored through the encrypted vault layer. Leave both empty for local unauthenticated access.",
			Schema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "username",
					Label:       "Username",
					Type:        connectors.FieldString,
					Description: "Optional Redis or Valkey ACL username.",
				},
				{
					Name:        "password",
					Label:       "Password",
					Type:        connectors.FieldSecret,
					Secret:      true,
					Description: "Optional Redis or Valkey password.",
				},
			}},
		},
	}
}

func (Connector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	product := serverFamilyLabel(serverFamily(target))
	title := product + " target"
	if strings.TrimSpace(target.Name) != "" {
		title = product + " target: " + target.Name
	}
	return connectors.ConnectorHelp{
		Title:       title,
		Summary:     "Browse Redis or Valkey keys and run bounded key operations through AIPermission approval rules.",
		Connector:   Label,
		ConnectorID: Kind,
		Usage: []string{
			"Use scan_keys before reading key values when the key name is unknown.",
			"Use get_key to read a bounded value preview by key type.",
			"Use set_string only for intentional string writes; non-string mutations should be explicit future actions.",
			"Use delete_keys carefully; it is destructive and should normally require approval.",
		},
		Warnings: []string{
			"Redis and Valkey values may contain secrets. Redaction is best-effort; avoid intentionally reading secrets unless the operator approved that access.",
			"scan_keys uses SCAN, not KEYS, and returns bounded batches.",
			"scan_keys limit is a target count, not a hard cap: the last page is kept whole, up to 5095 keys. Continue with next_cursor until complete; an empty page need not be complete.",
			"A scan stops after 100 pages and returns its continuation. Actions have a 10-second total deadline. SCAN is not a snapshot and may repeat keys while data changes.",
			"Credential profiles decide what the Redis-compatible server itself allows.",
			"Cluster-aware MOVED/ASK routing and Sentinel discovery are not supported in this connector version.",
		},
	}, nil
}

func (Connector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{
		{
			Name:        ActionPing,
			Label:       "Ping",
			Description: "Check Redis or Valkey connectivity and selected database access.",
			Category:    "metadata",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{},
			OutputHint:  connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
		{
			Name:        ActionInfo,
			Label:       "Server info",
			Description: "Read bounded Redis or Valkey INFO metadata.",
			Category:    "metadata",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "section", Label: "Section", Type: connectors.FieldString, Description: "Optional INFO section such as server, clients, memory, stats, or keyspace."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 128 << 10},
		},
		{
			Name:        ActionScanKeys,
			Label:       "Scan keys",
			Description: "List keys with SCAN using a bounded count.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "pattern", Label: "Pattern", Type: connectors.FieldString, Default: "*", Description: "MATCH pattern for SCAN."},
				{Name: "cursor", Label: "Cursor", Type: connectors.FieldString, Description: "Optional cursor returned by a previous scan."},
				{Name: "limit", Label: "Limit", Type: connectors.FieldInteger, Default: defaultScanLimit, Description: "Target key count. The final SCAN page is returned in full to avoid losing keys."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: maxScanResultKeys},
		},
		{
			Name:        ActionGetKey,
			Label:       "Read key",
			Description: "Read a bounded key preview by type.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true},
				{Name: "limit", Label: "Collection limit", Type: connectors.FieldInteger, Default: defaultValueLimit},
				{Name: "max_bytes", Label: "Max bytes", Type: connectors.FieldInteger, Default: defaultMaxValueBytes},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: maxValueBytes},
		},
		{
			Name:        ActionSetString,
			Label:       "Set string",
			Description: "Set a string value with optional TTL.",
			Category:    "write",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true},
				{Name: "value", Label: "Value", Type: connectors.FieldMultiline, Required: true},
				{Name: "ttl_seconds", Label: "TTL seconds", Type: connectors.FieldInteger, Description: "Optional positive TTL."},
			}},
			SensitiveInputFields: []string{"value"},
			OutputHint:           connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
		{
			Name:        ActionExpireKey,
			Label:       "Set TTL",
			Description: "Set or clear a key TTL.",
			Category:    "write",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true},
				{Name: "ttl_seconds", Label: "TTL seconds", Type: connectors.FieldInteger, Required: true, Description: "Positive seconds, or -1 to persist."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
		{
			Name:        ActionDeleteKeys,
			Label:       "Delete keys",
			Description: "Delete one or more keys.",
			Category:    "destructive",
			Risk:        connectors.RiskDestructive,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "keys", Label: "Keys", Type: connectors.FieldJSON, Required: true, Description: "JSON array of key names."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: 1000},
		},
	}, nil
}

func (Connector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	input := copyMap(req.Input)
	product := serverFamilyLabel(serverFamily(req.Target))
	risk := connectors.RiskRead
	title := ""
	summary := ""
	switch req.ActionName {
	case ActionPing:
		title = "Ping " + product
		summary = "Check Redis-compatible connectivity."
	case ActionInfo:
		section := strings.TrimSpace(stringValue(input, "section"))
		title = "Read " + product + " INFO"
		if section != "" {
			title += " " + section
		}
		summary = "Read bounded Redis-compatible metadata."
	case ActionScanKeys:
		pattern := normalizeStringDefault(input, "pattern", "*")
		limit := normalizeInt(input, "limit", defaultScanLimit, 1, maxScanLimit)
		input["pattern"] = pattern
		input["limit"] = limit
		title = "Scan " + product + " keys"
		summary = fmt.Sprintf("Scan keys matching %q with limit %d.", pattern, limit)
	case ActionGetKey:
		key := strings.TrimSpace(stringValue(input, "key"))
		if key == "" {
			return connectors.PreparedAction{}, fmt.Errorf("key is required")
		}
		input["key"] = key
		input["limit"] = normalizeInt(input, "limit", defaultValueLimit, 1, maxValueLimit)
		input["max_bytes"] = normalizeInt(input, "max_bytes", defaultMaxValueBytes, 1, maxValueBytes)
		title = "Read " + product + " key"
		summary = key
	case ActionSetString:
		risk = connectors.RiskWrite
		key := strings.TrimSpace(stringValue(input, "key"))
		if key == "" {
			return connectors.PreparedAction{}, fmt.Errorf("key is required")
		}
		if _, ok := input["value"]; !ok {
			return connectors.PreparedAction{}, fmt.Errorf("value is required")
		}
		input["key"] = key
		input["ttl_seconds"] = normalizeInt(input, "ttl_seconds", 0, 0, 31_536_000)
		title = "Set " + product + " string"
		summary = key
	case ActionExpireKey:
		risk = connectors.RiskWrite
		key := strings.TrimSpace(stringValue(input, "key"))
		if key == "" {
			return connectors.PreparedAction{}, fmt.Errorf("key is required")
		}
		ttl := normalizeInt(input, "ttl_seconds", 0, -1, 31_536_000)
		if ttl == 0 {
			return connectors.PreparedAction{}, fmt.Errorf("ttl_seconds is required")
		}
		input["key"] = key
		input["ttl_seconds"] = ttl
		title = "Set " + product + " TTL"
		summary = key
	case ActionDeleteKeys:
		risk = connectors.RiskDestructive
		keys, err := normalizeKeys(input["keys"])
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		input["keys"] = keys
		title = "Delete " + product + " keys"
		summary = fmt.Sprintf("Delete %d %s key(s).", len(keys), product)
	default:
		return connectors.PreparedAction{}, ErrUnsupportedAction
	}
	if len(req.Reason) > maxRedisCommandReason {
		return connectors.PreparedAction{}, fmt.Errorf("reason is too large")
	}
	return connectors.PreparedAction{
		ConnectorKind: Kind,
		TargetRef:     req.Target.Ref,
		ProfileID:     req.Profile.ID,
		ActionName:    req.ActionName,
		Dependencies:  connectors.NetworkTransportDependencies(req.Target),
		Risk:          risk,
		Title:         title,
		Summary:       summary,
		Preview:       input,
		Payload:       input,
		ContextMaterial: map[string]any{
			"target":          req.Target.Name,
			"profile":         req.Profile.Label,
			"server_family":   serverFamily(req.Target),
			"connection_mode": connectionMode(req.Target),
			"tls_mode":        redisTLSMode(req.Target),
			"database":        redisDatabase(req.Target),
		},
	}, nil
}
