package connectors

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FieldType describes a primitive UI/API schema field type.
type FieldType string

const (
	FieldString          FieldType = "string"
	FieldSecret          FieldType = "secret"
	FieldMultiline       FieldType = "multiline"
	FieldMultilineSecret FieldType = "multiline_secret"
	FieldNumber          FieldType = "number"
	FieldInteger         FieldType = "integer"
	FieldBoolean         FieldType = "boolean"
	FieldSelect          FieldType = "select"
	FieldJSON            FieldType = "json"
	FieldFileText        FieldType = "file_text"
	FieldFileBase64      FieldType = "file_base64"
)

// FieldOption describes a selectable field value.
type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Field describes one connector target, credential, or action input field.
type Field struct {
	Name        string        `json:"name"`
	Label       string        `json:"label"`
	Type        FieldType     `json:"type"`
	Required    bool          `json:"required,omitempty"`
	Secret      bool          `json:"secret,omitempty"`
	Description string        `json:"description,omitempty"`
	Default     any           `json:"default,omitempty"`
	Options     []FieldOption `json:"options,omitempty"`
}

// Schema is a small declarative shape used for target forms, credential forms,
// and action inputs.
type Schema struct {
	Fields []Field `json:"fields"`
}

// CredentialSchema describes one credential profile kind supported by a
// connector.
type CredentialSchema struct {
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Schema      Schema `json:"schema"`
}

// OutputHint gives core/UI a connector-provided hint for rendering and
// redaction. Core still owns the actual redaction behavior.
type OutputHint struct {
	Format                    string   `json:"format,omitempty"`
	SensitiveFields           []string `json:"sensitive_fields,omitempty"`
	TemporaryCapabilityFields []string `json:"temporary_capability_fields,omitempty"`
	MaxRows                   int      `json:"max_rows,omitempty"`
	MaxBytes                  int      `json:"max_bytes,omitempty"`
}

// ValidateSchemaValues validates a connector target/action value map against
// the connector-declared schema. It is intentionally small: connector code owns
// deep semantic validation, while core enforces required fields, unknown fields,
// primitive types, and select options.
func ValidateSchemaValues(schema Schema, values map[string]any) error {
	_, err := NormalizeSchemaValues(schema, values)
	return err
}

// NormalizeSchemaValues validates and returns a canonical copy of connector
// values. It keeps JSON persistence and approval-context hashes stable when
// equivalent clients send primitive values in different representations.
func NormalizeSchemaValues(schema Schema, values map[string]any) (map[string]any, error) {
	if values == nil {
		values = map[string]any{}
	}
	fields := map[string]Field{}
	for _, field := range schema.Fields {
		if strings.TrimSpace(field.Name) == "" {
			return nil, fmt.Errorf("connector schema contains a field without a name")
		}
		fields[field.Name] = field
	}
	for name := range values {
		if _, ok := fields[name]; !ok {
			return nil, fmt.Errorf("unsupported field %q", name)
		}
	}
	normalized := map[string]any{}
	for _, field := range schema.Fields {
		value, ok := values[field.Name]
		if (!ok || emptySchemaValue(value)) && field.Required && field.Default == nil {
			return nil, fmt.Errorf("%s is required", field.Name)
		}
		if !ok || emptySchemaValue(value) {
			if field.Default == nil {
				continue
			}
			value = field.Default
		}
		if err := validateFieldValue(field, value); err != nil {
			return nil, err
		}
		normalized[field.Name] = normalizeFieldValue(field, value)
	}
	return normalized, nil
}

func SchemaContainsSecret(schema Schema) bool {
	for _, field := range schema.Fields {
		if IsSecretField(field) {
			return true
		}
	}
	return false
}

func IsSecretField(field Field) bool {
	return field.Secret || field.Type == FieldSecret || field.Type == FieldMultilineSecret
}

func ValidateNonSecretSchema(schema Schema, usage string) error {
	if strings.TrimSpace(usage) == "" {
		usage = "connector"
	}
	return validateSchemaDefinition(schema, usage, false)
}

func ValidateCredentialSchemaDefinition(schema CredentialSchema) error {
	if !ValidIdentifier(schema.Kind) {
		return fmt.Errorf("invalid credential kind %q", schema.Kind)
	}
	return validateSchemaDefinition(schema.Schema, "credential "+schema.Kind, true)
}

func ValidateActionDefinitions(actions []ActionDefinition, usage string) error {
	if strings.TrimSpace(usage) == "" {
		usage = "connector actions"
	}
	seen := map[string]bool{}
	for _, action := range actions {
		if !ValidIdentifier(action.Name) {
			return fmt.Errorf("%s contains invalid action name %q", usage, action.Name)
		}
		if strings.TrimSpace(action.Label) == "" {
			return fmt.Errorf("%s action %q label is required", usage, action.Name)
		}
		if strings.TrimSpace(action.Description) == "" {
			return fmt.Errorf("%s action %q description is required", usage, action.Name)
		}
		if seen[action.Name] {
			return fmt.Errorf("%s contains duplicate action %q", usage, action.Name)
		}
		seen[action.Name] = true
		if !ValidRisk(action.Risk) {
			return fmt.Errorf("%s action %q has unsupported risk %q", usage, action.Name, action.Risk)
		}
		if err := ValidateNonSecretSchema(action.InputSchema, usage+" action "+action.Name+" input"); err != nil {
			return err
		}
		inputFields := make(map[string]bool, len(action.InputSchema.Fields))
		for _, field := range action.InputSchema.Fields {
			inputFields[field.Name] = true
		}
		seenSensitiveFields := map[string]bool{}
		for _, field := range action.SensitiveInputFields {
			if !inputFields[field] {
				return fmt.Errorf("%s action %q sensitive input field %q is not in its input schema", usage, action.Name, field)
			}
			if seenSensitiveFields[field] {
				return fmt.Errorf("%s action %q contains duplicate sensitive input field %q", usage, action.Name, field)
			}
			seenSensitiveFields[field] = true
		}
		seenSensitiveOutputFields := map[string]bool{}
		for _, field := range action.OutputHint.SensitiveFields {
			field = strings.TrimSpace(field)
			if !ValidIdentifier(field) {
				return fmt.Errorf("%s action %q contains invalid sensitive output field %q", usage, action.Name, field)
			}
			if seenSensitiveOutputFields[field] {
				return fmt.Errorf("%s action %q contains duplicate sensitive output field %q", usage, action.Name, field)
			}
			seenSensitiveOutputFields[field] = true
		}
		seenCapabilityFields := map[string]bool{}
		for _, field := range action.OutputHint.TemporaryCapabilityFields {
			field = strings.TrimSpace(field)
			if !ValidIdentifier(field) {
				return fmt.Errorf("%s action %q contains invalid temporary capability field %q", usage, action.Name, field)
			}
			if seenSensitiveOutputFields[field] {
				return fmt.Errorf("%s action %q temporary capability field %q is also marked sensitive", usage, action.Name, field)
			}
			if seenCapabilityFields[field] {
				return fmt.Errorf("%s action %q contains duplicate temporary capability field %q", usage, action.Name, field)
			}
			seenCapabilityFields[field] = true
		}
	}
	return nil
}

func ValidRisk(risk RiskLevel) bool {
	switch risk {
	case RiskRead, RiskWrite, RiskDestructive, RiskCredentialSensitive:
		return true
	default:
		return false
	}
}

func validateSchemaDefinition(schema Schema, usage string, allowSecrets bool) error {
	seen := map[string]bool{}
	for _, field := range schema.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			return fmt.Errorf("%s schema contains a field without a name", usage)
		}
		if seen[name] {
			return fmt.Errorf("%s schema contains duplicate field %q", usage, name)
		}
		seen[name] = true
		if err := validateFieldDefinition(field, usage); err != nil {
			return err
		}
		if !allowSecrets && IsSecretField(field) {
			return fmt.Errorf("%s schema field %q must not be secret; store secrets in credential profiles instead", usage, field.Name)
		}
		if allowSecrets && (field.Type == FieldSecret || field.Type == FieldMultilineSecret) && !field.Secret {
			return fmt.Errorf("%s schema field %q uses a secret field type and must set secret=true", usage, field.Name)
		}
		if allowSecrets && IsSecretField(field) && field.Default != nil {
			return fmt.Errorf("%s schema field %q is secret and must not declare a default value", usage, field.Name)
		}
	}
	return nil
}

func validateFieldDefinition(field Field, usage string) error {
	switch field.Type {
	case FieldString, FieldSecret, FieldMultiline, FieldMultilineSecret, FieldNumber, FieldInteger, FieldBoolean, FieldSelect, FieldJSON, FieldFileText, FieldFileBase64:
	default:
		return fmt.Errorf("%s schema field %q has unsupported field type %q", usage, field.Name, field.Type)
	}
	if field.Default != nil {
		if err := validateFieldValue(field, field.Default); err != nil {
			return fmt.Errorf("%s schema field %q has an invalid default: %w", usage, field.Name, err)
		}
	}
	if field.Type == FieldSelect {
		seen := map[string]bool{}
		for _, option := range field.Options {
			if strings.TrimSpace(option.Value) == "" {
				return fmt.Errorf("%s schema field %q has an empty select option value", usage, field.Name)
			}
			if seen[option.Value] {
				return fmt.Errorf("%s schema field %q has duplicate select option %q", usage, field.Name, option.Value)
			}
			seen[option.Value] = true
		}
	}
	return nil
}

// ValidateCredentialSchemaValues validates public and secret credential maps
// against one credential schema. Secret fields are read from the secret map;
// non-secret fields are read from the public map.
func ValidateCredentialSchemaValues(schema Schema, public map[string]any, secret map[string]any, requireSecrets bool) error {
	if public == nil {
		public = map[string]any{}
	}
	if secret == nil {
		secret = map[string]any{}
	}
	publicFields := map[string]Field{}
	secretFields := map[string]Field{}
	for _, field := range schema.Fields {
		if strings.TrimSpace(field.Name) == "" {
			return fmt.Errorf("connector credential schema contains a field without a name")
		}
		if IsSecretField(field) {
			secretFields[field.Name] = field
		} else {
			publicFields[field.Name] = field
		}
	}
	for name := range public {
		if _, ok := publicFields[name]; !ok {
			return fmt.Errorf("unsupported public credential field %q", name)
		}
	}
	for name := range secret {
		if _, ok := secretFields[name]; !ok {
			return fmt.Errorf("unsupported secret credential field %q", name)
		}
	}
	for _, field := range schema.Fields {
		values := public
		required := field.Required
		if IsSecretField(field) {
			values = secret
			required = field.Required && requireSecrets
		}
		value, ok := values[field.Name]
		if (!ok || emptySchemaValue(value)) && required && field.Default == nil {
			return fmt.Errorf("%s is required", field.Name)
		}
		if !ok || emptySchemaValue(value) {
			continue
		}
		if err := validateFieldValue(field, value); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldValue(field Field, value any) error {
	switch field.Type {
	case FieldString, FieldSecret, FieldMultiline, FieldMultilineSecret, FieldFileText, FieldFileBase64:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", field.Name)
		}
	case FieldNumber:
		if !schemaNumber(value) {
			return fmt.Errorf("%s must be a number", field.Name)
		}
	case FieldInteger:
		if _, ok := normalizeIntegerValue(value); !ok {
			return fmt.Errorf("%s must be an exact integer in the supported range", field.Name)
		}
	case FieldBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", field.Name)
		}
	case FieldSelect:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", field.Name)
		}
		if len(field.Options) > 0 {
			for _, option := range field.Options {
				if option.Value == text {
					return nil
				}
			}
			return fmt.Errorf("%s has unsupported value %q", field.Name, text)
		}
	case FieldJSON:
		return nil
	default:
		return fmt.Errorf("%s has unsupported field type %q", field.Name, field.Type)
	}
	return nil
}

func normalizeFieldValue(field Field, value any) any {
	switch field.Type {
	case FieldNumber:
		number, ok := normalizeNumberValue(value)
		if ok {
			return number
		}
	case FieldInteger:
		integer, ok := normalizeIntegerValue(value)
		if ok {
			return integer
		}
	}
	return value
}

func emptySchemaValue(value any) bool {
	if value == nil {
		return true
	}
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) == ""
}

func schemaNumber(value any) bool {
	_, ok := normalizeNumberValue(value)
	return ok
}

func normalizeNumberValue(value any) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case int:
		number = float64(typed)
	case int8:
		number = float64(typed)
	case int16:
		number = float64(typed)
	case int32:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case uint:
		number = float64(typed)
	case uint8:
		number = float64(typed)
	case uint16:
		number = float64(typed)
	case uint32:
		number = float64(typed)
	case uint64:
		number = float64(typed)
	case float32:
		number = float64(typed)
	case float64:
		number = typed
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func normalizeIntegerValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		if uint64(typed) > math.MaxInt64 {
			return 0, false
		}
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		if typed > math.MaxInt64 {
			return 0, false
		}
		return int64(typed), true
	case float32:
		return exactFloatInteger(float64(typed))
	case float64:
		return exactFloatInteger(typed)
	case json.Number:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

// NativeIntValue converts an exact connector integer to the current platform's
// int width without truncation.
func NativeIntValue(value any) (int, bool) {
	parsed, ok := normalizeIntegerValue(value)
	if !ok {
		return 0, false
	}
	native, err := strconv.Atoi(strconv.FormatInt(parsed, 10))
	if err != nil {
		return 0, false
	}
	return native, true
}

func exactFloatInteger(value float64) (int64, bool) {
	const maxExactFloatInteger = 1<<53 - 1
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value ||
		value < -maxExactFloatInteger || value > maxExactFloatInteger {
		return 0, false
	}
	return int64(value), true
}
