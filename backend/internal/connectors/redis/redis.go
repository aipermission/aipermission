// Package redisconnector defines the Redis connector contract.
package redisconnector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Kind    = "redis"
	Label   = "Redis"
	Version = "0.2"

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
			Description: "Redis host as seen by the selected connection mode. For Over SSH this is usually 127.0.0.1 on the remote server.",
		},
		{
			Name:        "port",
			Label:       "Port",
			Type:        connectors.FieldNumber,
			Required:    true,
			Default:     defaultRedisPort,
			Description: "Redis TCP port.",
		},
		{
			Name:        "database",
			Label:       "Database",
			Type:        connectors.FieldNumber,
			Default:     0,
			Description: "Redis logical database number.",
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
			Description: "Redis ACL username and password stored through the encrypted vault layer. Leave both empty for local unauthenticated Redis.",
			Schema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "username",
					Label:       "Username",
					Type:        connectors.FieldString,
					Description: "Optional Redis ACL username.",
				},
				{
					Name:        "password",
					Label:       "Password",
					Type:        connectors.FieldSecret,
					Secret:      true,
					Description: "Optional Redis password.",
				},
			}},
		},
	}
}

func (Connector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	title := "Redis target"
	if strings.TrimSpace(target.Name) != "" {
		title = "Redis target: " + target.Name
	}
	return connectors.ConnectorHelp{
		Title:       title,
		Summary:     "Browse Redis keys and run bounded key operations through AIPermission approval rules.",
		Connector:   Label,
		ConnectorID: Kind,
		Usage: []string{
			"Use scan_keys before reading key values when the key name is unknown.",
			"Use get_key to read a bounded value preview by key type.",
			"Use set_string only for intentional string writes; non-string mutations should be explicit future actions.",
			"Use delete_keys carefully; it is destructive and should normally require approval.",
		},
		Warnings: []string{
			"Redis values may contain secrets. Redaction is best-effort; avoid intentionally reading secrets unless the operator approved that access.",
			"scan_keys uses SCAN, not KEYS, and returns bounded batches.",
			"Redis credential profiles decide what the Redis server itself allows.",
		},
	}, nil
}

func (Connector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{
		{
			Name:        ActionPing,
			Label:       "Ping",
			Description: "Check Redis connectivity and selected database access.",
			Category:    "metadata",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{},
			OutputHint:  connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
		{
			Name:        ActionInfo,
			Label:       "Server info",
			Description: "Read bounded Redis INFO metadata.",
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
			Description: "List Redis keys with SCAN using a bounded count.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "pattern", Label: "Pattern", Type: connectors.FieldString, Default: "*", Description: "MATCH pattern for SCAN."},
				{Name: "cursor", Label: "Cursor", Type: connectors.FieldString, Description: "Optional cursor returned by a previous scan."},
				{Name: "limit", Label: "Limit", Type: connectors.FieldNumber, Default: defaultScanLimit, Description: "Maximum keys to return."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: maxScanLimit},
		},
		{
			Name:        ActionGetKey,
			Label:       "Read key",
			Description: "Read a bounded Redis key preview by type.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true},
				{Name: "limit", Label: "Collection limit", Type: connectors.FieldNumber, Default: defaultValueLimit},
				{Name: "max_bytes", Label: "Max bytes", Type: connectors.FieldNumber, Default: defaultMaxValueBytes},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: maxValueBytes},
		},
		{
			Name:        ActionSetString,
			Label:       "Set string",
			Description: "Set a Redis string value with optional TTL.",
			Category:    "write",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true},
				{Name: "value", Label: "Value", Type: connectors.FieldMultiline, Required: true},
				{Name: "ttl_seconds", Label: "TTL seconds", Type: connectors.FieldNumber, Description: "Optional positive TTL."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
		{
			Name:        ActionExpireKey,
			Label:       "Set TTL",
			Description: "Set or clear a Redis key TTL.",
			Category:    "write",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true},
				{Name: "ttl_seconds", Label: "TTL seconds", Type: connectors.FieldNumber, Required: true, Description: "Positive seconds, or -1 to persist."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
		{
			Name:        ActionDeleteKeys,
			Label:       "Delete keys",
			Description: "Delete one or more Redis keys.",
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
	risk := connectors.RiskRead
	title := ""
	summary := ""
	switch req.ActionName {
	case ActionPing:
		title = "Ping Redis"
		summary = "Check Redis connectivity."
	case ActionInfo:
		section := strings.TrimSpace(stringValue(input, "section"))
		title = "Read Redis INFO"
		if section != "" {
			title = "Read Redis INFO " + section
		}
		summary = "Read bounded Redis metadata."
	case ActionScanKeys:
		pattern := normalizeStringDefault(input, "pattern", "*")
		limit := normalizeInt(input, "limit", defaultScanLimit, 1, maxScanLimit)
		input["pattern"] = pattern
		input["limit"] = limit
		title = "Scan Redis keys"
		summary = fmt.Sprintf("Scan keys matching %q with limit %d.", pattern, limit)
	case ActionGetKey:
		key := strings.TrimSpace(stringValue(input, "key"))
		if key == "" {
			return connectors.PreparedAction{}, fmt.Errorf("key is required")
		}
		input["key"] = key
		input["limit"] = normalizeInt(input, "limit", defaultValueLimit, 1, maxValueLimit)
		input["max_bytes"] = normalizeInt(input, "max_bytes", defaultMaxValueBytes, 1, maxValueBytes)
		title = "Read Redis key"
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
		title = "Set Redis string"
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
		title = "Set Redis TTL"
		summary = key
	case ActionDeleteKeys:
		risk = connectors.RiskDestructive
		keys, err := normalizeKeys(input["keys"])
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		input["keys"] = keys
		title = "Delete Redis keys"
		summary = fmt.Sprintf("Delete %d Redis key(s).", len(keys))
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
			"connection_mode": connectionMode(req.Target),
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
	return connectors.TestResult{
		Status:  connectors.TestOK,
		Message: "Redis connection ok.",
		Details: map[string]any{
			"response": respString(value),
			"database": redisDatabase(runtime.Target),
		},
	}, nil
}

func openRedisClient(ctx context.Context, runtime connectors.RuntimeContext) (*redisClient, error) {
	transport, _ := runtime.Capability(connectors.NetworkTransportCapabilityName).(connectors.NetworkTransport)
	if transport == nil {
		return nil, ErrMissingTransport
	}
	conn, err := transport.DialConnectorTCP(ctx, connectors.NetworkDialRequest{
		Mode:               connectionMode(runtime.Target),
		Host:               redisHost(runtime.Target),
		Port:               redisPort(runtime.Target),
		TransportTargetRef: strings.TrimSpace(stringValue(runtime.Target.Config, "transport_target_ref")),
	})
	if err != nil {
		return nil, err
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
	if err != nil {
		password = ""
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
	parsed := parseRedisInfo(info)
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"section": section, "info": parsed, "raw": info},
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
		if value.kind != respArray || len(value.array) != 2 {
			return connectors.ActionResult{}, fmt.Errorf("unexpected SCAN response")
		}
		nextCursor = respString(value.array[0])
		keys = append(keys, respStringSlice(value.array[1])...)
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
	ttl := int64(-2)
	if ttlValue, err := client.Do("PTTL", key); err == nil {
		ttl = ttlValue.number
	}
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
		output["value"] = limitStringMap(respStringMap(value), limit, maxBytes)
	case "list":
		value, err := client.Do("LRANGE", key, "0", strconv.Itoa(limit-1))
		if err != nil {
			return connectors.ActionResult{}, err
		}
		output["value"] = limitStrings(respStringSlice(value), limit, maxBytes)
	case "set":
		output["value"], _ = redisScanCollection(client, "SSCAN", key, limit, maxBytes)
	case "zset":
		value, err := client.Do("ZRANGE", key, "0", strconv.Itoa(limit-1), "WITHSCORES")
		if err != nil {
			return connectors.ActionResult{}, err
		}
		output["value"] = scorePairs(respStringSlice(value), maxBytes)
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
		DisplayText: fmt.Sprintf("Set Redis key %q.", key),
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
		DisplayText: fmt.Sprintf("Updated TTL for Redis key %q.", key),
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
		DisplayText: fmt.Sprintf("Deleted %d Redis key(s).", value.number),
	}, nil
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
		if value.kind != respArray || len(value.array) != 2 {
			return nil, fmt.Errorf("unexpected %s response", command)
		}
		cursor = respString(value.array[0])
		items = append(items, limitStrings(respStringSlice(value.array[1]), limit-len(items), maxBytes)...)
		if cursor == "0" {
			break
		}
	}
	return items, nil
}

func parseRedisInfo(raw string) map[string]any {
	sections := map[string]any{}
	current := "default"
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			current = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			if _, ok := sections[current]; !ok {
				sections[current] = map[string]string{}
			}
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		bucket, _ := sections[current].(map[string]string)
		if bucket == nil {
			bucket = map[string]string{}
			sections[current] = bucket
		}
		bucket[parts[0]] = parts[1]
	}
	return sections
}

func redisKeyDisplay(output map[string]any) string {
	encoded := fmt.Sprintf("%v", output["value"])
	return truncateString(encoded, 4000)
}

func scorePairs(values []string, maxBytes int) []map[string]string {
	out := []map[string]string{}
	for index := 0; index+1 < len(values); index += 2 {
		out = append(out, map[string]string{
			"member": truncateString(values[index], maxBytes),
			"score":  values[index+1],
		})
	}
	return out
}

func classifyRedisTestError(err error) connectors.TestStatus {
	if err == nil {
		return connectors.TestOK
	}
	message := strings.ToLower(err.Error())
	switch {
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

var _ connectors.Connector = Connector{}
var _ connectors.TestableConnector = Connector{}
