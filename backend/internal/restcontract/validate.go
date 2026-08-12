package restcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ValidateTypedResponse validates a real handler response against the
// hand-reviewed typed subset published in the generated OpenAPI document.
func ValidateTypedResponse(method string, path string, statusCode int, body []byte) error {
	route := Route{Method: strings.ToUpper(strings.TrimSpace(method)), Path: path}
	contract, ok := typedOperationContracts()[route]
	if !ok {
		return fmt.Errorf("route %s %s has no typed response contract", route.Method, route.Path)
	}
	if strconv.Itoa(statusCode) != contract.StatusCode {
		return fmt.Errorf("route %s %s returned status %d, want %s", route.Method, route.Path, statusCode, contract.StatusCode)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode response JSON: %w", err)
	}
	return validateSchemaValue("$", value, contract.ResponseSchema, sharedSchemas())
}

func validateSchemaValue(path string, value any, schema map[string]any, schemas map[string]any) error {
	if ref, ok := schema["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		resolved, ok := schemas[name].(map[string]any)
		if !ok || name == ref {
			return fmt.Errorf("%s references unknown schema %q", path, ref)
		}
		return validateSchemaValue(path, value, resolved, schemas)
	}
	if allowed, ok := schema["enum"].([]string); ok {
		text, ok := value.(string)
		if !ok || !containsString(allowed, text) {
			return fmt.Errorf("%s value %q is outside enum %v", path, value, allowed)
		}
	}
	schemaType, _ := schema["type"].(string)
	switch schemaType {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		properties, _ := schema["properties"].(map[string]any)
		if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
			for key := range object {
				if properties[key] == nil {
					return fmt.Errorf("%s contains undocumented property %q", path, key)
				}
			}
		}
		if required, ok := schema["required"].([]string); ok {
			for _, key := range required {
				if _, exists := object[key]; !exists {
					return fmt.Errorf("%s is missing required property %q", path, key)
				}
			}
		}
		for key, child := range object {
			childSchema, ok := properties[key].(map[string]any)
			if !ok {
				continue
			}
			if err := validateSchemaValue(path+"."+key, child, childSchema, schemas); err != nil {
				return err
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		itemSchema, _ := schema["items"].(map[string]any)
		for index, item := range items {
			if err := validateSchemaValue(fmt.Sprintf("%s[%d]", path, index), item, itemSchema, schemas); err != nil {
				return err
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		if schema["format"] == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
				return fmt.Errorf("%s must be an RFC3339 date-time: %w", path, err)
			}
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be an integer", path)
		}
		if _, err := number.Int64(); err != nil {
			return fmt.Errorf("%s must be an integer: %w", path, err)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	}
	return nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
