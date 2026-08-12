package diagnostics

import (
	"regexp"
	"strings"
	"time"
)

var safeIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func safeIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if safeIdentifierPattern.MatchString(value) {
		return value
	}
	return "unknown"
}

func safeTimestamp(value string) string {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func classifyError(status string, value string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "outcome_unknown":
		return "outcome_unknown"
	case "blocked":
		return "permission"
	case "stale":
		return "context_drift"
	}
	value = strings.ToLower(value)
	switch {
	case containsAny(value, "context deadline", "deadline exceeded", "timed out", "timeout"):
		return "timeout"
	case containsAny(value, "x509", "certificate", "tls", "ssl"):
		return "tls"
	case containsAny(value, "authentication", "password", "credential", "login", "sasl"):
		return "authentication"
	case containsAny(value, "permission denied", "forbidden", "unauthorized", "not allowed", "access denied"):
		return "authorization"
	case containsAny(value, "connection refused", "no route to host", "network is unreachable", "dial tcp", "broken pipe", "connection reset", "unexpected eof"):
		return "network"
	case containsAny(value, "not found", "does not exist", "no such"):
		return "not_found"
	case containsAny(value, "conflict", "already exists", "stale", "changed in another"):
		return "conflict"
	case containsAny(value, "invalid", "required", "validation", "unsupported"):
		return "validation"
	case containsAny(value, "internal"):
		return "internal"
	default:
		return "other"
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
