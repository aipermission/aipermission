// Package redisconnector defines the Redis connector contract.
package redisconnector

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Kind    = "redis"
	Label   = "Redis / Valkey"
	Version = "0.2"

	ServerFamilyRedis  = "redis"
	ServerFamilyValkey = "valkey"

	ActionPing       = "ping"
	ActionInfo       = "info"
	ActionScanKeys   = "scan_keys"
	ActionGetKey     = "get_key"
	ActionSetString  = "set_string"
	ActionExpireKey  = "expire_key"
	ActionDeleteKeys = "delete_keys"

	defaultRedisHost      = "127.0.0.1"
	defaultRedisPort      = 6379
	defaultScanLimit      = 100
	maxScanLimit          = 1000
	defaultValueLimit     = 256
	maxValueLimit         = 1000
	defaultMaxValueBytes  = 128 << 10
	maxValueBytes         = 512 << 10
	maxRedisCommandReason = 2000
)

var (
	ErrUnsupportedAction = errors.New("unsupported redis connector action")
	ErrMissingTransport  = errors.New("redis connector network transport is unavailable")
	ErrMissingSecret     = errors.New("redis connector credential is missing required secret")
	ErrInvalidConfig     = errors.New("redis connector target config is invalid")
)

// Connector describes Redis as a connector-shaped target with bounded key
// browsing and explicit write/destructive actions.
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
				{Name: "limit", Label: "Limit", Type: connectors.FieldInteger, Default: defaultScanLimit, Description: "Maximum keys to return."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: maxScanLimit},
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

func (Connector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	client, err := openRedisClient(ctx, runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer client.Close()
	switch action.ActionName {
	case ActionPing:
		return executePing(client)
	case ActionInfo:
		return executeInfo(client, action.Payload)
	case ActionScanKeys:
		return executeScanKeys(client, action.Payload)
	case ActionGetKey:
		return executeGetKey(client, action.Payload)
	case ActionSetString:
		return executeSetString(client, action.Payload)
	case ActionExpireKey:
		return executeExpireKey(client, action.Payload)
	case ActionDeleteKeys:
		return executeDeleteKeys(client, action.Payload)
	default:
		return connectors.ActionResult{}, ErrUnsupportedAction
	}
}

func (connector Connector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	client, err := openRedisClient(ctx, runtime)
	if err != nil {
		return connectors.TestResult{Status: classifyRedisTestError(err), Message: err.Error()}, nil
	}
	defer client.Close()
	value, err := client.Do("PING")
	if err != nil {
		return connectors.TestResult{Status: classifyRedisTestError(err), Message: err.Error()}, nil
	}
	configuredFamily := serverFamily(runtime.Target)
	details := map[string]any{
		"response":                 respString(value),
		"database":                 redisDatabase(runtime.Target),
		"configured_server_family": configuredFamily,
	}
	detectedFamily := configuredFamily
	message := serverFamilyLabel(configuredFamily) + " connection ok."
	if identity, detectErr := detectRedisServer(client); detectErr == nil {
		detectedFamily = identity.Family
		for key, item := range identity.details() {
			details[key] = item
		}
		details["server_family_match"] = detectedFamily == configuredFamily
		message = serverFamilyLabel(detectedFamily) + " connection ok."
		if detectedFamily != configuredFamily {
			message = fmt.Sprintf(
				"%s connection ok; target is configured as %s.",
				serverFamilyLabel(detectedFamily),
				serverFamilyLabel(configuredFamily),
			)
		}
	} else {
		details["server_detection"] = "unavailable"
	}
	return connectors.TestResult{
		Status:  connectors.TestOK,
		Message: message,
		Details: details,
	}, nil
}

func openRedisClient(ctx context.Context, runtime connectors.RuntimeContext) (*redisClient, error) {
	transport, _ := runtime.Capability(connectors.NetworkTransportCapabilityName).(connectors.NetworkTransport)
	if transport == nil {
		return nil, ErrMissingTransport
	}
	conn, err := transport.DialConnectorTCP(ctx, connectors.NetworkDialRequest{
		SourceTargetRef:    runtime.Target.Ref,
		SourceProjectID:    runtime.Target.ProjectID,
		Mode:               connectionMode(runtime.Target),
		Host:               redisHost(runtime.Target),
		Port:               redisPort(runtime.Target),
		TransportTargetRef: strings.TrimSpace(stringValue(runtime.Target.Config, "transport_target_ref")),
	})
	if err != nil {
		return nil, err
	}
	if tlsConfig := redisTLSConfig(runtime.Target); tlsConfig != nil {
		handshakeContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		tlsConn := tls.Client(conn, tlsConfig)
		handshakeErr := tlsConn.HandshakeContext(handshakeContext)
		cancel()
		if handshakeErr != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("redis TLS handshake: %w", handshakeErr)
		}
		conn = tlsConn
	}
	client := newRedisClient(conn)
	if err := authenticateRedis(ctx, runtime, client); err != nil {
		_ = client.Close()
		return nil, err
	}
	if database := redisDatabase(runtime.Target); database > 0 {
		if _, err := client.Do("SELECT", strconv.Itoa(database)); err != nil {
			_ = client.Close()
			return nil, err
		}
	}
	return client, nil
}

func authenticateRedis(ctx context.Context, runtime connectors.RuntimeContext, client *redisClient) error {
	username := strings.TrimSpace(stringValue(runtime.Profile.Public, "username"))
	password, err := runtime.Secrets.GetSecret(ctx, "password")
	if errors.Is(err, connectors.ErrSecretNotFound) {
		password = ""
	} else if err != nil {
		return fmt.Errorf("%w: resolve redis password: %w", connectors.ErrSecretProvider, err)
	}
	password = strings.TrimSpace(password)
	if username == "" && password == "" {
		return nil
	}
	if password == "" {
		return ErrMissingSecret
	}
	if username != "" {
		_, err = client.Do("AUTH", username, password)
	} else {
		_, err = client.Do("AUTH", password)
	}
	return err
}

func executePing(client *redisClient) (connectors.ActionResult, error) {
	value, err := client.Do("PING")
	if err != nil {
		return connectors.ActionResult{}, err
	}
	response := respString(value)
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"response": response},
		DisplayText: response,
	}, nil
}

func executeInfo(client *redisClient, input map[string]any) (connectors.ActionResult, error) {
	section := strings.TrimSpace(stringValue(input, "section"))
	args := []string{"INFO"}
	if section != "" {
		args = append(args, section)
	}
	value, err := client.Do(args...)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	info := truncateString(respString(value), maxValueBytes)
	document := parseRedisInfoDocument(info)
	output := map[string]any{"section": section, "info": document.sections, "raw": info}
	if identity, ok := redisServerIdentityFromFields(document.fields); ok {
		output["server"] = identity.details()
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: info,
	}, nil
}

func executeScanKeys(client *redisClient, input map[string]any) (connectors.ActionResult, error) {
	pattern := normalizeStringDefault(input, "pattern", "*")
	cursor := normalizeStringDefault(input, "cursor", "0")
	limit := normalizeInt(input, "limit", defaultScanLimit, 1, maxScanLimit)
	keys := []string{}
	nextCursor := cursor
	for len(keys) < limit {
		args := []string{"SCAN", nextCursor, "MATCH", pattern, "COUNT", strconv.Itoa(min(limit-len(keys), 100))}
		value, err := client.Do(args...)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		nextCursor, page, err := redisScanPage(value, "SCAN")
		if err != nil {
			return connectors.ActionResult{}, err
		}
		keys = append(keys, page...)
		if nextCursor == "0" {
			break
		}
	}
	if len(keys) > limit {
		keys = keys[:limit]
	}
	sort.Strings(keys)
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"pattern":     pattern,
			"cursor":      cursor,
			"next_cursor": nextCursor,
			"keys":        keys,
			"count":       len(keys),
			"complete":    nextCursor == "0",
		},
		DisplayText: strings.Join(keys, "\n"),
	}, nil
}

func executeGetKey(client *redisClient, input map[string]any) (connectors.ActionResult, error) {
	key := strings.TrimSpace(stringValue(input, "key"))
	if key == "" {
		return connectors.ActionResult{}, fmt.Errorf("key is required")
	}
	limit := normalizeInt(input, "limit", defaultValueLimit, 1, maxValueLimit)
	maxBytes := normalizeInt(input, "max_bytes", defaultMaxValueBytes, 1, maxValueBytes)
	keyType, err := redisKeyType(client, key)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	ttlValue, err := client.Do("PTTL", key)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if ttlValue.kind != respInteger {
		return connectors.ActionResult{}, fmt.Errorf("unexpected PTTL response: expected an integer")
	}
	ttl := ttlValue.number
	output := map[string]any{"key": key, "type": keyType, "ttl_ms": ttl}
	switch keyType {
	case "none":
		output["exists"] = false
	case "string":
		value, err := client.Do("GET", key)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		text := truncateString(respString(value), maxBytes)
		output["value"] = text
		output["truncated"] = len(respString(value)) > maxBytes
	case "hash":
		value, err := client.Do("HGETALL", key)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		fields, err := redisStringMap(value, "HGETALL")
		if err != nil {
			return connectors.ActionResult{}, err
		}
		output["value"] = limitStringMap(fields, limit, maxBytes)
	case "list":
		value, err := client.Do("LRANGE", key, "0", strconv.Itoa(limit-1))
		if err != nil {
			return connectors.ActionResult{}, err
		}
		items, err := redisStringSlice(value, "LRANGE")
		if err != nil {
			return connectors.ActionResult{}, err
		}
		output["value"] = limitStrings(items, limit, maxBytes)
	case "set":
		items, err := redisScanCollection(client, "SSCAN", key, limit, maxBytes)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		output["value"] = items
	case "zset":
		value, err := client.Do("ZRANGE", key, "0", strconv.Itoa(limit-1), "WITHSCORES")
		if err != nil {
			return connectors.ActionResult{}, err
		}
		items, err := redisStringSlice(value, "ZRANGE")
		if err != nil {
			return connectors.ActionResult{}, err
		}
		pairs, err := scorePairs(items, maxBytes)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		output["value"] = pairs
	default:
		output["value"] = fmt.Sprintf("Preview for Redis type %q is not supported yet.", keyType)
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: redisKeyDisplay(output),
	}, nil
}

func executeSetString(client *redisClient, input map[string]any) (connectors.ActionResult, error) {
	key := strings.TrimSpace(stringValue(input, "key"))
	value := fmt.Sprint(input["value"])
	if key == "" {
		return connectors.ActionResult{}, fmt.Errorf("key is required")
	}
	args := []string{"SET", key, value}
	if ttl := normalizeInt(input, "ttl_seconds", 0, 0, 31_536_000); ttl > 0 {
		args = append(args, "EX", strconv.Itoa(ttl))
	}
	response, err := client.Do(args...)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"key": key, "response": respString(response)},
		DisplayText: fmt.Sprintf("Set key %q.", key),
	}, nil
}

func executeExpireKey(client *redisClient, input map[string]any) (connectors.ActionResult, error) {
	key := strings.TrimSpace(stringValue(input, "key"))
	ttl := normalizeInt(input, "ttl_seconds", 0, -1, 31_536_000)
	if key == "" {
		return connectors.ActionResult{}, fmt.Errorf("key is required")
	}
	var value respValue
	var err error
	if ttl < 0 {
		value, err = client.Do("PERSIST", key)
	} else {
		value, err = client.Do("EXPIRE", key, strconv.Itoa(ttl))
	}
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"key": key, "changed": value.number == 1, "ttl_seconds": ttl},
		DisplayText: fmt.Sprintf("Updated TTL for key %q.", key),
	}, nil
}

func executeDeleteKeys(client *redisClient, input map[string]any) (connectors.ActionResult, error) {
	keys, err := normalizeKeys(input["keys"])
	if err != nil {
		return connectors.ActionResult{}, err
	}
	args := append([]string{"DEL"}, keys...)
	value, err := client.Do(args...)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"keys":    keys,
			"deleted": value.number,
		},
		DisplayText: fmt.Sprintf("Deleted %d key(s).", value.number),
	}, nil
}

type redisServerIdentity struct {
	Family               string
	ServerName           string
	Version              string
	CompatibilityVersion string
}

func detectRedisServer(client *redisClient) (redisServerIdentity, error) {
	value, err := client.Do("INFO", "server")
	if err != nil {
		return redisServerIdentity{}, err
	}
	identity, ok := redisServerIdentityFromInfo(respString(value))
	if !ok {
		return redisServerIdentity{}, fmt.Errorf("server identity is unavailable")
	}
	return identity, nil
}

func redisServerIdentityFromInfo(raw string) (redisServerIdentity, bool) {
	return redisServerIdentityFromFields(parseRedisInfoDocument(raw).fields)
}

func redisServerIdentityFromFields(fields map[string]string) (redisServerIdentity, bool) {
	serverName := strings.ToLower(strings.TrimSpace(fields["server_name"]))
	valkeyVersion := strings.TrimSpace(fields["valkey_version"])
	redisVersion := strings.TrimSpace(fields["redis_version"])
	if serverName == ServerFamilyValkey || valkeyVersion != "" {
		return redisServerIdentity{
			Family:               ServerFamilyValkey,
			ServerName:           firstNonEmpty(serverName, ServerFamilyValkey),
			Version:              valkeyVersion,
			CompatibilityVersion: redisVersion,
		}, true
	}
	if serverName != "" || redisVersion != "" {
		return redisServerIdentity{
			Family:     ServerFamilyRedis,
			ServerName: firstNonEmpty(serverName, ServerFamilyRedis),
			Version:    redisVersion,
		}, true
	}
	return redisServerIdentity{}, false
}

func (identity redisServerIdentity) details() map[string]any {
	details := map[string]any{
		"detected_server_family": identity.Family,
		"server_name":            identity.ServerName,
	}
	if identity.Version != "" {
		details["server_version"] = identity.Version
	}
	if identity.CompatibilityVersion != "" {
		details["compatibility_version"] = identity.CompatibilityVersion
	}
	return details
}

type redisInfoDocument struct {
	sections map[string]any
	fields   map[string]string
}

func parseRedisInfoDocument(raw string) redisInfoDocument {
	document := redisInfoDocument{
		sections: map[string]any{},
		fields:   map[string]string{},
	}
	current := "default"
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			current = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			if _, ok := document.sections[current]; !ok {
				document.sections[current] = map[string]string{}
			}
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		document.fields[strings.ToLower(key)] = value
		bucket, _ := document.sections[current].(map[string]string)
		if bucket == nil {
			bucket = map[string]string{}
			document.sections[current] = bucket
		}
		bucket[key] = value
	}
	return document
}

func redisKeyType(client *redisClient, key string) (string, error) {
	value, err := client.Do("TYPE", key)
	if err != nil {
		return "", err
	}
	return respString(value), nil
}

func redisScanCollection(client *redisClient, command string, key string, limit int, maxBytes int) ([]string, error) {
	cursor := "0"
	items := []string{}
	for len(items) < limit {
		value, err := client.Do(command, key, cursor, "COUNT", strconv.Itoa(min(limit-len(items), 100)))
		if err != nil {
			return nil, err
		}
		nextCursor, page, err := redisScanPage(value, command)
		if err != nil {
			return nil, err
		}
		cursor = nextCursor
		items = append(items, limitStrings(page, limit-len(items), maxBytes)...)
		if cursor == "0" {
			break
		}
	}
	return items, nil
}

func redisScanPage(value respValue, command string) (string, []string, error) {
	if value.kind != respArray || value.null || len(value.array) != 2 {
		return "", nil, fmt.Errorf("unexpected %s response: expected cursor and items", command)
	}
	cursorValue := value.array[0]
	if cursorValue.kind != respSimpleString && cursorValue.kind != respBulkString {
		return "", nil, fmt.Errorf("unexpected %s cursor response", command)
	}
	items, err := redisStringSlice(value.array[1], command)
	if err != nil {
		return "", nil, err
	}
	return respString(cursorValue), items, nil
}

func redisKeyDisplay(output map[string]any) string {
	encoded := fmt.Sprintf("%v", output["value"])
	return truncateString(encoded, 4000)
}

func scorePairs(values []string, maxBytes int) ([]map[string]string, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("unexpected ZRANGE response: member and score pairs are incomplete")
	}
	out := []map[string]string{}
	for index := 0; index+1 < len(values); index += 2 {
		out = append(out, map[string]string{
			"member": truncateString(values[index], maxBytes),
			"score":  values[index+1],
		})
	}
	return out, nil
}

func redisStringSlice(value respValue, command string) ([]string, error) {
	if value.kind != respArray || value.null {
		return nil, fmt.Errorf("unexpected %s response: expected an array", command)
	}
	out := make([]string, 0, len(value.array))
	for _, item := range value.array {
		if item.kind != respSimpleString && item.kind != respBulkString && item.kind != respInteger {
			return nil, fmt.Errorf("unexpected %s response: expected scalar array items", command)
		}
		out = append(out, respString(item))
	}
	return out, nil
}

func redisStringMap(value respValue, command string) (map[string]string, error) {
	items, err := redisStringSlice(value, command)
	if err != nil {
		return nil, err
	}
	if len(items)%2 != 0 {
		return nil, fmt.Errorf("unexpected %s response: field and value pairs are incomplete", command)
	}
	out := make(map[string]string, len(items)/2)
	for index := 0; index < len(items); index += 2 {
		out[items[index]] = items[index+1]
	}
	return out, nil
}

func classifyRedisTestError(err error) connectors.TestStatus {
	if err == nil {
		return connectors.TestOK
	}
	if errors.Is(err, connectors.ErrSecretProvider) {
		return connectors.TestUnknownError
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "tls"), strings.Contains(message, "certificate"), strings.Contains(message, "x509"):
		return connectors.TestFailedTLS
	case strings.Contains(message, "auth"), strings.Contains(message, "noauth"), strings.Contains(message, "invalid username-password"):
		return connectors.TestFailedAuth
	case strings.Contains(message, "connection refused"), strings.Contains(message, "i/o timeout"), strings.Contains(message, "no such host"), strings.Contains(message, "network"):
		return connectors.TestFailedNetwork
	default:
		return connectors.TestUnknownError
	}
}

func connectionMode(target connectors.TargetView) string {
	mode := strings.TrimSpace(stringValue(target.Config, "connection_mode"))
	if mode == "" {
		return "direct"
	}
	return mode
}

func redisTLSConfig(target connectors.TargetView) *tls.Config {
	mode := redisTLSMode(target)
	useTLS := mode == "verify_full"
	if mode == "auto" {
		useTLS = connectors.UseVerifiedTLSByDefault(connectionMode(target), redisHost(target))
	}
	if !useTLS {
		return nil
	}
	return connectors.VerifiedTLSConfig(redisHost(target))
}

func redisTLSMode(target connectors.TargetView) string {
	switch strings.TrimSpace(stringValue(target.Config, "tls_mode")) {
	case "auto":
		return "auto"
	case "verify_full":
		return "verify_full"
	default:
		return "disable"
	}
}

func serverFamily(target connectors.TargetView) string {
	if strings.EqualFold(strings.TrimSpace(stringValue(target.Config, "server_family")), ServerFamilyValkey) {
		return ServerFamilyValkey
	}
	return ServerFamilyRedis
}

func serverFamilyLabel(family string) string {
	if family == ServerFamilyValkey {
		return "Valkey"
	}
	return "Redis"
}

func redisHost(target connectors.TargetView) string {
	host := strings.TrimSpace(stringValue(target.Config, "host"))
	if host == "" {
		return defaultRedisHost
	}
	return host
}

func redisPort(target connectors.TargetView) int {
	return normalizeInt(target.Config, "port", defaultRedisPort, 1, 65535)
}

func redisDatabase(target connectors.TargetView) int {
	return normalizeInt(target.Config, "database", 0, 0, 1023)
}

func normalizeStringDefault(input map[string]any, key string, fallback string) string {
	value := strings.TrimSpace(stringValue(input, key))
	if value == "" {
		return fallback
	}
	return value
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value := values[key]
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func normalizeInt(values map[string]any, key string, fallback int, minValue int, maxValue int) int {
	if values == nil {
		return fallback
	}
	value, ok := values[key]
	if !ok || value == nil || value == "" {
		return fallback
	}
	var parsed int
	switch typed := value.(type) {
	case int:
		parsed = typed
	case int64:
		parsed = int(typed)
	case float64:
		parsed = int(typed)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return fallback
		}
		parsed = n
	default:
		return fallback
	}
	if parsed < minValue {
		return minValue
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}

func normalizeKeys(value any) ([]string, error) {
	raw, ok := value.([]any)
	if !ok {
		if stringsValue, ok := value.([]string); ok {
			raw = make([]any, 0, len(stringsValue))
			for _, item := range stringsValue {
				raw = append(raw, item)
			}
		}
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("keys must be a non-empty array")
	}
	keys := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		key := strings.TrimSpace(fmt.Sprint(item))
		if key == "" || seen[key] {
			continue
		}
		keys = append(keys, key)
		seen[key] = true
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("keys must be a non-empty array")
	}
	if len(keys) > maxScanLimit {
		return nil, fmt.Errorf("too many keys")
	}
	return keys, nil
}

func copyMap(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func limitStrings(values []string, limit int, maxBytes int) []string {
	if limit < 1 || len(values) == 0 {
		return nil
	}
	if len(values) > limit {
		values = values[:limit]
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, truncateString(value, maxBytes))
	}
	return out
}

func limitStringMap(values map[string]string, limit int, maxBytes int) map[string]string {
	out := map[string]string{}
	if limit < 1 {
		return out
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	for _, key := range keys {
		out[key] = truncateString(values[key], maxBytes)
	}
	return out
}

func truncateString(value string, maxBytes int) string {
	if maxBytes < 1 || len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "...[truncated]"
}

func min(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var _ connectors.Connector = Connector{}
var _ connectors.TestableConnector = Connector{}
