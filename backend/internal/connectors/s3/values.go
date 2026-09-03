package s3connector

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func normalizeObjectKey(input map[string]any, name string) string {
	key := strings.TrimSpace(stringValue(input, name))
	key = strings.TrimLeft(key, "/")
	return key
}

func objectFilename(key string) string {
	return lastPathSegment(key, "s3-object")
}

func directoryName(prefix string) string {
	return lastPathSegment(prefix, prefix)
}

func lastPathSegment(value string, fallback string) string {
	parts := strings.Split(strings.TrimRight(value, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[len(parts)-1]) == "" {
		return fallback
	}
	return parts[len(parts)-1]
}

func intValue(values map[string]any, name string) int {
	return clampedInt(values, name, 0, -int(^uint(0)>>1)-1, int(^uint(0)>>1))
}

func s3Scheme(target connectors.TargetView) string {
	scheme := strings.ToLower(strings.TrimSpace(stringValue(target.Config, "scheme")))
	if scheme != "http" && scheme != "https" {
		return defaultS3Scheme
	}
	return scheme
}

func s3Host(target connectors.TargetView) string {
	host := strings.TrimSpace(stringValue(target.Config, "host"))
	if host == "" {
		return defaultS3Host
	}
	return host
}

func s3Port(target connectors.TargetView) int {
	defaultPort := defaultS3Port
	if s3Scheme(target) == "http" {
		defaultPort = 80
	}
	return clampedInt(target.Config, "port", defaultPort, 1, 65535)
}

func s3Region(target connectors.TargetView) string {
	region := strings.TrimSpace(stringValue(target.Config, "region"))
	if region == "" {
		return defaultS3Region
	}
	return region
}

func s3Bucket(target connectors.TargetView) string {
	return strings.TrimSpace(stringValue(target.Config, "bucket"))
}

func s3PathStyle(target connectors.TargetView) bool {
	value, ok := target.Config["path_style"]
	if !ok {
		return true
	}
	return boolish(value)
}

func s3TrustConditionalRequests(target connectors.TargetView) bool {
	value, ok := target.Config["trust_conditional_requests"]
	if !ok {
		return false
	}
	trusted, ok := value.(bool)
	return ok && trusted
}

func connectionMode(target connectors.TargetView) string {
	mode := strings.TrimSpace(stringValue(target.Config, "connection_mode"))
	if mode == "" {
		return "direct"
	}
	return mode
}

func copyMap(value map[string]any) map[string]any {
	out := map[string]any{}
	for key, item := range value {
		out[key] = item
	}
	return out
}

func stringValue(values map[string]any, name string) string {
	if values == nil {
		return ""
	}
	switch value := values[name].(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	case float64:
		if value == float64(int64(value)) {
			return strconv.FormatInt(int64(value), 10)
		}
		return strconv.FormatFloat(value, 'f', -1, 64)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case bool:
		return strconv.FormatBool(value)
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func clampedInt(values map[string]any, name string, fallback int, minValue int, maxValue int) int {
	value, ok := values[name]
	if !ok || value == nil || value == "" {
		return fallback
	}
	parsed := fallback
	switch typed := value.(type) {
	case int:
		parsed = typed
	case int64:
		parsed = int(typed)
	case float64:
		parsed = int(typed)
	case string:
		if candidate, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			parsed = candidate
		}
	}
	if parsed < minValue {
		return minValue
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}

func boolValue(values map[string]any, name string) bool {
	if values == nil {
		return false
	}
	return boolish(values[name])
}

func boolish(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	case float64:
		return typed != 0
	case int:
		return typed != 0
	default:
		return false
	}
}
