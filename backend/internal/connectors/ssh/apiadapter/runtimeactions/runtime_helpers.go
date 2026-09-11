package runtimeactions

import (
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/console"
)

func ExecOutput(result console.ExecResult) map[string]any {
	return map[string]any{
		"command":     result.Command,
		"stdout":      console.PlainOutput(result.Output),
		"stderr":      "",
		"exit_code":   result.ExitCode,
		"running":     result.Running,
		"session_id":  result.SessionID,
		"duration_ms": result.DurationMS,
	}
}

func ExactSessionHandle(id, generation int64) console.SessionHandle {
	return console.SessionHandle{ID: id, Generation: generation}
}

func stringPayload(payload map[string]any, name string) string {
	value, ok := payload[name]
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

func intPayload(payload map[string]any, name string, fallback int) int {
	value, ok := payload[name]
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
	default:
		return fallback
	}
}

func stringSlicePayload(payload map[string]any, name string) []string {
	value, ok := payload[name]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return out
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	default:
		return []string{fmt.Sprint(typed)}
	}
}

func normalizeRemoteDirectoryPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("path cannot contain control characters")
	}
	return value, nil
}
