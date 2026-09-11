package transport

import "fmt"

func stringPayload(payload map[string]any, name string) string {
	value, ok := payload[name]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}
