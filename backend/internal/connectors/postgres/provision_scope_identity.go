package postgresconnector

import "fmt"

// SQL identifiers in GRANT statements must not be silently truncated by the
// server or interpreted as SQL tokens. Metadata lookup parameters are separate.
func provisionScopeIdentifier(input map[string]any, name string) (string, error) {
	value, err := metadataIdentifierInput(input, name)
	if err != nil {
		return "", err
	}
	if value == "" || len(value) > 63 {
		return "", fmt.Errorf("scope %s must be an exact identifier of 1 to 63 UTF-8 bytes", name)
	}
	return value, nil
}

func provisionScopeColumns(value any) ([]string, error) {
	var values []any
	switch typed := value.(type) {
	case []any:
		values = typed
	case []string:
		values = make([]any, len(typed))
		for index, column := range typed {
			values[index] = column
		}
	default:
		return nil, fmt.Errorf("scope columns must be an array of exact identifiers")
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("selected table must grant all columns or at least one column")
	}
	columns := make([]string, len(values))
	for index, column := range values {
		name, err := provisionScopeIdentifier(map[string]any{"column": column}, "column")
		if err != nil {
			return nil, err
		}
		columns[index] = name
	}
	return columns, nil
}
