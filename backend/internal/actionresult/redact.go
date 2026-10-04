package actionresult

import (
	"encoding/json"
	"fmt"
)

type RedactionOptions struct {
	SensitiveField           func(string) bool
	TemporaryCapabilityField func(string) bool
	RedactText               func(string) string
	RedactKey                func(string) string
	RedactCapability         func(string) string
	// RedactScalar checks registered credentials, not optional text patterns.
	RedactScalar func(string) string
}

// CanonicalizeAndRedact establishes the connector result boundary in one
// operation and revalidates the final projection after replacement markers are
// applied.
func CanonicalizeAndRedact(value any, limits Limits, options RedactionOptions) (any, error) {
	return CanonicalizeAndRedactWithSourceLimits(value, limits, limits, options)
}

// CanonicalizeAndRedactWithSourceLimits permits a larger bounded source value
// when declared sensitive fields will be replaced before the final projection.
func CanonicalizeAndRedactWithSourceLimits(value any, sourceLimits Limits, projectionLimits Limits, options RedactionOptions) (any, error) {
	canonical, err := Canonicalize(value, sourceLimits)
	if err != nil {
		return nil, err
	}
	redacted, err := Redact(canonical, options)
	if err != nil {
		return nil, err
	}
	return Canonicalize(redacted, projectionLimits)
}

// Redact traverses a canonical JSON value and applies the same persistence
// policy to string leaves and mandatory credential checks to scalar leaves.
// Declared temporary capabilities preserve their
// signed syntax while still honoring operator-defined custom redaction.
func Redact(value any, options RedactionOptions) (any, error) {
	return redactValue(value, options, true)
}

func redactValue(value any, options RedactionOptions, allowCapabilities bool) (any, error) {
	switch typed := value.(type) {
	case nil:
		return value, nil
	case bool, json.Number:
		return redactScalar(typed, options.RedactScalar), nil
	case string:
		return redactText(options.RedactText, typed), nil
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			redacted, err := redactValue(item, options, allowCapabilities)
			if err != nil {
				return nil, err
			}
			out = append(out, redacted)
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			redactedKey := redactText(options.RedactKey, key)
			if _, exists := out[redactedKey]; exists {
				return nil, fmt.Errorf("%w: redaction produced duplicate object key", ErrInvalidValue)
			}
			if options.SensitiveField != nil && options.SensitiveField(key) {
				out[redactedKey] = "[REDACTED]"
				continue
			}
			if allowCapabilities && options.TemporaryCapabilityField != nil && options.TemporaryCapabilityField(key) {
				redacted, err := redactCapabilityValue(item, options)
				if err != nil {
					return nil, err
				}
				out[redactedKey] = redacted
				continue
			}
			redacted, err := redactValue(item, options, allowCapabilities)
			if err != nil {
				return nil, err
			}
			out[redactedKey] = redacted
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: redaction received a non-canonical type", ErrInvalidValue)
	}
}

func redactCapabilityValue(value any, options RedactionOptions) (any, error) {
	switch typed := value.(type) {
	case string:
		return redactText(options.RedactCapability, typed), nil
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			redacted, err := redactCapabilityValue(item, options)
			if err != nil {
				return nil, err
			}
			out = append(out, redacted)
		}
		return out, nil
	default:
		return redactValue(value, options, false)
	}
}

func redactText(redactor func(string) string, value string) string {
	if redactor == nil {
		return value
	}
	return redactor(value)
}

func redactScalar(value any, redactor func(string) string) any {
	if redactor != nil {
		encoded, err := json.Marshal(value)
		if err == nil && redactor(string(encoded)) != string(encoded) {
			// Never emit a partly redacted numeric spelling or a rounded substitute.
			return CredentialRedactionMarker
		}
	}
	return value
}
