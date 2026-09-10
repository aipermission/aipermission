package actionresult

import (
	"context"
	"errors"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

var ErrRedactorUnavailable = errors.New("connector action redactor is unavailable")

type TextRedactor func(context.Context, string) string

// Redactor owns connector action projection policy. Workspace-level secret
// discovery remains behind the injected text redactors.
type Redactor struct {
	persistText    TextRedactor
	capabilityText TextRedactor
	inputBytes     int
}

func NewRedactor(persistText TextRedactor, capabilityText TextRedactor, inputBytes int) (*Redactor, error) {
	if persistText == nil || capabilityText == nil || inputBytes < 1 {
		return nil, ErrRedactorUnavailable
	}
	return &Redactor{persistText: persistText, capabilityText: capabilityText, inputBytes: inputBytes}, nil
}

func (r *Redactor) Value(ctx context.Context, value any, sensitiveFields map[string]bool, capabilityFields map[string]bool) (any, error) {
	return r.ValueWithLimits(ctx, value, sensitiveFields, capabilityFields, CredentialBoundary{}, DefaultLimits())
}

func (r *Redactor) ValueWithCredentialBoundary(ctx context.Context, value any, sensitiveFields map[string]bool, capabilityFields map[string]bool, boundary CredentialBoundary) (any, error) {
	return r.ValueWithLimits(ctx, value, sensitiveFields, capabilityFields, boundary, DefaultLimits())
}

func (r *Redactor) ValueWithLimits(ctx context.Context, value any, sensitiveFields map[string]bool, capabilityFields map[string]bool, boundary CredentialBoundary, sourceLimits Limits) (any, error) {
	if r == nil || r.persistText == nil || r.capabilityText == nil {
		return nil, ErrRedactorUnavailable
	}
	return CanonicalizeAndRedactWithSourceLimits(value, sourceLimits, DefaultLimits(), RedactionOptions{
		SensitiveField: func(key string) bool {
			return outputFieldSensitive(key, sensitiveFields)
		},
		TemporaryCapabilityField: func(key string) bool {
			return outputFieldDeclared(key, capabilityFields)
		},
		RedactText: func(value string) string {
			return boundary.Redact(r.persistText(ctx, value))
		},
		RedactKey: func(value string) string {
			return boundary.RedactKey(r.persistText(ctx, value))
		},
		RedactCapability: func(value string) string {
			return boundary.Redact(r.capabilityText(ctx, value))
		},
	})
}

func (r *Redactor) Result(ctx context.Context, result connectors.ActionResult, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	return r.ResultWithCredentialBoundary(ctx, result, CredentialBoundary{}, hints...)
}

func (r *Redactor) Text(ctx context.Context, value string, boundary CredentialBoundary) (string, error) {
	if r == nil || r.persistText == nil {
		return "", ErrRedactorUnavailable
	}
	return boundary.Redact(r.persistText(ctx, value)), nil
}

func (r *Redactor) ResultWithCredentialBoundary(ctx context.Context, result connectors.ActionResult, boundary CredentialBoundary, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	if r == nil || r.persistText == nil {
		return connectors.ActionResult{}, ErrRedactorUnavailable
	}
	sensitiveFields := SensitiveOutputFields(hints...)
	capabilityFields := temporaryCapabilityFields(hints...)
	result.DisplayText, _ = r.Text(ctx, result.DisplayText, boundary)
	result.Error, _ = r.Text(ctx, result.Error, boundary)
	redacted, err := r.ValueWithCredentialBoundary(ctx, result.Output, sensitiveFields, capabilityFields, boundary)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	result.Output = redacted
	if result.Metadata != nil {
		redactedMetadata, err := r.ValueWithCredentialBoundary(ctx, result.Metadata, sensitiveFields, capabilityFields, boundary)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		result.Metadata, _ = redactedMetadata.(map[string]any)
	}
	return result, nil
}

func (r *Redactor) Input(ctx context.Context, input map[string]any, sensitiveInputFields []string) (map[string]any, error) {
	if input == nil {
		return map[string]any{}, nil
	}
	fields := SensitiveOutputFields()
	for _, field := range sensitiveInputFields {
		if normalized := NormalizeField(field); normalized != "" {
			fields[normalized] = true
		}
	}
	value, err := r.ValueWithLimits(ctx, input, fields, nil, CredentialBoundary{}, r.inputRedactionLimits())
	if err != nil {
		return nil, err
	}
	redacted, ok := value.(map[string]any)
	if !ok || redacted == nil {
		return nil, ErrInvalidValue
	}
	return redacted, nil
}

func (r *Redactor) Preview(ctx context.Context, preview map[string]any, sensitiveFields []string, hints ...connectors.OutputHint) (map[string]any, error) {
	if preview == nil {
		return map[string]any{}, nil
	}
	fields := SensitiveOutputFields(hints...)
	for _, field := range sensitiveFields {
		if normalized := NormalizeField(field); normalized != "" {
			fields[normalized] = true
		}
	}
	value, err := r.ValueWithLimits(ctx, preview, fields, nil, CredentialBoundary{}, r.inputRedactionLimits())
	if err != nil {
		return nil, err
	}
	redacted, ok := value.(map[string]any)
	if !ok || redacted == nil {
		return nil, ErrInvalidValue
	}
	return redacted, nil
}

func (r *Redactor) inputRedactionLimits() Limits {
	limits := DefaultLimits()
	limits.EncodedBytes = r.inputBytes
	limits.StringBytes = r.inputBytes
	return limits
}

func temporaryCapabilityFields(hints ...connectors.OutputHint) map[string]bool {
	fields := map[string]bool{}
	for _, hint := range hints {
		for _, field := range hint.TemporaryCapabilityFields {
			if normalized := NormalizeField(field); normalized != "" {
				fields[normalized] = true
			}
		}
	}
	return fields
}

func SensitiveOutputFields(hints ...connectors.OutputHint) map[string]bool {
	fields := map[string]bool{
		"api_key": true, "api_token_hash": true, "apikey": true, "authorization": true,
		"credential": true, "credential_hash": true, "credential_value": true,
		"password": true, "password_hash": true, "private_key": true, "refresh_token": true,
		"secret": true, "secret_access_key": true, "secret_hash": true, "secret_value": true,
		"token": true, "token_hash": true,
	}
	for _, hint := range hints {
		for _, field := range hint.SensitiveFields {
			if normalized := NormalizeField(field); normalized != "" {
				fields[normalized] = true
			}
		}
	}
	return fields
}

func outputFieldSensitive(key string, sensitiveFields map[string]bool) bool {
	normalized := NormalizeField(key)
	if normalized == "" {
		return false
	}
	if sensitiveFields[normalized] {
		return true
	}
	for field := range sensitiveFields {
		if strings.HasSuffix(normalized, "."+field) || strings.HasSuffix(normalized, "_"+field) {
			return true
		}
	}
	return false
}

func outputFieldDeclared(key string, fields map[string]bool) bool {
	normalized := NormalizeField(key)
	return normalized != "" && fields[normalized]
}
