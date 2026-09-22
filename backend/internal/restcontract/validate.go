package restcontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ValidateTypedResponse validates a real handler response against the
// hand-reviewed typed subset published in the generated OpenAPI document.
func ValidateTypedResponse(method string, path string, statusCode int, body []byte) error {
	route := Route{Method: strings.ToUpper(strings.TrimSpace(method)), Path: path}
	contract, ok := typedOperationContracts()[route]
	if !ok {
		return fmt.Errorf("route %s %s has no typed response contract", route.Method, route.Path)
	}
	status := strconv.Itoa(statusCode)
	schema := contract.ResponseSchema
	if status != contract.StatusCode {
		schema = contract.AdditionalResponses[status]
	}
	if schema == nil {
		return fmt.Errorf("route %s %s returned status %d, want %s", route.Method, route.Path, statusCode, contract.StatusCode)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode response JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode response JSON: multiple values are not allowed")
		}
		return fmt.Errorf("decode response JSON: %w", err)
	}
	return validateSchemaValue("$", value, schema, sharedSchemas())
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
	if expected, ok := schema["const"]; ok {
		actualJSON, actualErr := json.Marshal(value)
		expectedJSON, expectedErr := json.Marshal(expected)
		if actualErr != nil || expectedErr != nil || !bytes.Equal(actualJSON, expectedJSON) {
			return fmt.Errorf("%s value %q does not equal constant %q", path, value, expected)
		}
	}
	if err := validateSchemaComposition(path, value, schema, schemas); err != nil {
		return err
	}
	if allowed, ok := schema["enum"].([]string); ok {
		text, ok := value.(string)
		if !ok || !containsString(allowed, text) {
			return fmt.Errorf("%s value %q is outside enum %v", path, value, allowed)
		}
	}
	schemaType, _ := schema["type"].(string)
	if schemaType == "object" || schema["properties"] != nil || schema["required"] != nil || schema["additionalProperties"] != nil {
		object, ok := value.(map[string]any)
		if !ok {
			if schemaType == "object" {
				return fmt.Errorf("%s must be an object", path)
			}
		} else if err := validateObjectSchema(path, object, schema, schemas); err != nil {
			return err
		}
	}
	switch schemaType {
	case "object":
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
		return validateStringSchema(path, value, schema)
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

func validateSchemaComposition(path string, value any, schema map[string]any, schemas map[string]any) error {
	if branches, ok := schema["allOf"].([]any); ok {
		for _, raw := range branches {
			branch, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("%s contains an invalid allOf branch", path)
			}
			if err := validateSchemaValue(path, value, branch, schemas); err != nil {
				return err
			}
		}
	}
	if branches, ok := schema["anyOf"].([]any); ok {
		matched := false
		for _, raw := range branches {
			branch, ok := raw.(map[string]any)
			if ok && validateSchemaValue(path, value, branch, schemas) == nil {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s does not match any allowed schema", path)
		}
	}
	condition, ok := schema["if"].(map[string]any)
	if !ok {
		return nil
	}
	branchName := "else"
	if validateSchemaValue(path, value, condition, schemas) == nil {
		branchName = "then"
	}
	branch, _ := schema[branchName].(map[string]any)
	if branch == nil {
		return nil
	}
	return validateSchemaValue(path, value, branch, schemas)
}

func validateObjectSchema(path string, object map[string]any, schema map[string]any, schemas map[string]any) error {
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
	return nil
}

func validateStringSchema(path string, value any, schema map[string]any) error {
	text, ok := value.(string)
	if !ok {
		return fmt.Errorf("%s must be a string", path)
	}
	if schema["format"] == "date-time" {
		if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
			return fmt.Errorf("%s must be an RFC3339 date-time: %w", path, err)
		}
	}
	if minimum, ok := schema["minLength"].(int); ok && utf8.RuneCountInString(text) < minimum {
		return fmt.Errorf("%s must contain at least %d character(s)", path, minimum)
	}
	if pattern, ok := schema["pattern"].(string); ok {
		matched, err := regexp.MatchString(pattern, text)
		if err != nil || !matched {
			return fmt.Errorf("%s must match pattern %q", path, pattern)
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
