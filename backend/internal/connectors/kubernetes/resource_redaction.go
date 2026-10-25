package kubernetesconnector

// Resource describes are metadata reads, not a route for delivering inline
// credentials. Traverse pod templates, init/ephemeral containers and annotations
// without changing their remaining structure or the caller's source object.
func redactResource(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			switch key {
			case "kubectl.kubernetes.io/last-applied-configuration":
				result[key] = "***"
			case "env":
				result[key] = redactResource(item)
				if entries, ok := result[key].([]any); ok {
					for _, entry := range entries {
						if env, ok := entry.(map[string]any); ok {
							if _, exists := env["value"]; exists {
								env["value"] = "***"
							}
						}
					}
				}
			default:
				result[key] = redactResource(item)
			}
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = redactResource(item)
		}
		return result
	default:
		return value
	}
}
