package connectors

import (
	"encoding/json"
	"fmt"
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
	Format          string   `json:"format,omitempty"`
	SensitiveFields []string `json:"sensitive_fields,omitempty"`
	MaxRows         int      `json:"max_rows,omitempty"`
	MaxBytes        int      `json:"max_bytes,omitempty"`
}

// ValidateSchemaValues validates a connector target/action value map against
// the connector-declared schema. It is intentionally small: connector code owns
// deep semantic validation, while core enforces required fields, unknown fields,
// primitive types, and select options.
func ValidateSchemaValues(schema Schema, values map[string]any) error {
	if values == nil {
		values = map[string]any{}
	}
	fields := map[string]Field{}
	for _, field := range schema.Fields {
		if strings.TrimSpace(field.Name) == "" {
			return fmt.Errorf("connector schema contains a field without a name")
		}
		fields[field.Name] = field
	}
	for name := range values {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("unsupported field %q", name)
		}
	}
	for _, field := range schema.Fields {
		value, ok := values[field.Name]
		if (!ok || emptySchemaValue(value)) && field.Required && field.Default == nil {
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
	case FieldString, FieldSecret, FieldMultiline, FieldMultilineSecret, FieldNumber, FieldBoolean, FieldSelect, FieldJSON, FieldFileText, FieldFileBase64:
	default:
		return fmt.Errorf("%s schema field %q has unsupported field type %q", usage, field.Name, field.Type)
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

func emptySchemaValue(value any) bool {
	if value == nil {
		return true
	}
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) == ""
}

func schemaNumber(value any) bool {
	switch typed := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	case json.Number:
		_, err := typed.Float64()
		return err == nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return false
		}
		_, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return err == nil
	default:
		return false
	}
}
