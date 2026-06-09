package connectors

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
