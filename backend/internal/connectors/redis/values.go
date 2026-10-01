package redisconnector

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/boundedtext"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

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
	cursor := respString(cursorValue)
	if _, err := strconv.ParseUint(cursor, 10, 64); err != nil || cursorValue.null || cursor == "" {
		return "", nil, fmt.Errorf("unexpected %s cursor response", command)
	}
	return cursor, items, nil
}

func redisKeyDisplay(output map[string]any) string {
	if output["exists"] == false {
		return "Redis key does not exist."
	}
	if text, ok := output["value"].(string); ok {
		return truncateString(text, 4000)
	}
	return fmt.Sprintf("Read Redis %s preview (%v item(s), truncated=%v).", output["type"], output["returned_items"], output["truncated"])
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
	return connectors.StringMapValue(values, key)
}

func normalizeInt(values map[string]any, key string, fallback int, minValue int, maxValue int) int {
	return connectors.BoundedIntMapValue(values, key, fallback, minValue, maxValue)
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
		key, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("keys must contain only strings")
		}
		if key == "" {
			return nil, fmt.Errorf("keys must not contain an empty key")
		}
		if err := validateRedisKeyIdentity(key); err != nil {
			return nil, err
		}
		if seen[key] {
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

func exactRedisKey(values map[string]any, field string) (string, error) {
	key, ok := values[field].(string)
	if !ok || key == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if err := validateRedisKeyIdentity(key); err != nil {
		return "", err
	}
	return key, nil
}

func copyMap(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func truncateString(value string, maxBytes int) string {
	return boundedtext.TruncateUTF8(value, maxBytes, "...[truncated]")
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
