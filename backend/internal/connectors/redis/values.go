package redisconnector

import (
	"crypto/tls"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

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

func redisScanCollection(client *redisClient, command string, key string, limit int, maxBytes int) ([]string, error) {
	cursor := "0"
	items := []string{}
	for pages := 0; len(items) < limit; pages++ {
		if pages == maxScanPages {
			return nil, fmt.Errorf("redis collection scan exceeded %d pages", maxScanPages)
		}
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
	cursor := respString(cursorValue)
	if _, err := strconv.ParseUint(cursor, 10, 64); err != nil || cursorValue.null || cursor == "" {
		return "", nil, fmt.Errorf("unexpected %s cursor response", command)
	}
	return cursor, items, nil
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
