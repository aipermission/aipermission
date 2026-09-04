package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// A connector may intentionally preview two separately bounded 64 KiB bodies.
// JSON escaping can expand those strings by up to 6x, so the generic envelope
// must bound the encoded preview rather than assume source byte size.
const maxPreparedActionPreviewBytes = 1 << 20

var (
	ErrTargetNotFound       = errors.New("target not found")
	ErrConnectorUnavailable = errors.New("connector unavailable")
)

// TargetResolver resolves a stable target reference into the public target and
// selected credential profile used for a connector action.
//
// Refs include the connector kind, target id, and credential profile id, for
// example "postgres:7:11" or "redis:12:3".
type TargetResolver interface {
	ResolveActionTarget(ctx context.Context, targetRef string) (ResolvedTarget, error)
}

// ResolvedTarget is the non-secret target/profile pair for one action request.
type ResolvedTarget struct {
	Target  connectors.TargetView
	Profile connectors.CredentialProfileView
}

// PrepareRequest is the gateway-facing request to prepare a connector action.
type PrepareRequest struct {
	Source    string
	TargetRef string

	ActionName string
	Input      map[string]any
	Reason     string
	CreatedAt  time.Time
}

// PreparedRequest is ready for permission evaluation and approval.
type PreparedRequest struct {
	Target           connectors.TargetView
	Profile          connectors.CredentialProfileView
	ConnectorVersion string
	ActionDefinition connectors.ActionDefinition
	Action           connectors.PreparedAction
	Requested        PrepareRequest
	// IdempotencyInput preserves the caller-supplied shape before schema
	// defaults are applied, so changing a payload while reusing a key conflicts.
	IdempotencyInput map[string]any
	Dependencies     []ResolvedDependency
}

// ResolvedDependency is a non-secret target/profile snapshot whose state can
// affect execution of the prepared action.
type ResolvedDependency struct {
	Purpose string
	Target  connectors.TargetView
	Profile connectors.CredentialProfileView
}

// Service owns the generic target -> connector -> prepared action boundary.
type Service struct {
	registry *connectors.Registry
	targets  TargetResolver
}

// NewService creates a generic connector action service.
func NewService(registry *connectors.Registry, targets TargetResolver) *Service {
	return &Service{registry: registry, targets: targets}
}

// Prepare resolves the target/profile, finds the connector, and asks the
// connector to prepare an action without executing it.
func (s *Service) Prepare(ctx context.Context, request PrepareRequest) (PreparedRequest, error) {
	if s == nil || s.registry == nil || s.targets == nil {
		return PreparedRequest{}, fmt.Errorf("action service is not configured")
	}
	if request.TargetRef == "" {
		return PreparedRequest{}, fmt.Errorf("target_ref is required")
	}
	if !connectors.ValidIdentifier(request.ActionName) {
		return PreparedRequest{}, fmt.Errorf("invalid action name %q", request.ActionName)
	}

	resolved, err := s.targets.ResolveActionTarget(ctx, request.TargetRef)
	if err != nil {
		return PreparedRequest{}, err
	}
	if resolved.Target.ConnectorKind == "" {
		return PreparedRequest{}, fmt.Errorf("target %q has no connector kind", request.TargetRef)
	}

	connector, ok := s.registry.Get(resolved.Target.ConnectorKind)
	if !ok {
		return PreparedRequest{}, fmt.Errorf("%w: %s", ErrConnectorUnavailable, resolved.Target.ConnectorKind)
	}
	actionDefinition, err := connectorActionDefinition(ctx, connector, resolved.Target, resolved.Profile, request.ActionName)
	if err != nil {
		return PreparedRequest{}, err
	}
	if connectors.SchemaContainsSecret(actionDefinition.InputSchema) {
		return PreparedRequest{}, fmt.Errorf("connector action input schema %q includes secret fields; store secrets in credential profiles instead", request.ActionName)
	}
	idempotencyInput := request.Input
	if idempotencyInput == nil {
		idempotencyInput = map[string]any{}
	}
	input, err := connectors.NormalizeSchemaValues(actionDefinition.InputSchema, request.Input)
	if err != nil {
		return PreparedRequest{}, redactSensitivePreparationError(err, request.Input, actionDefinition.SensitiveInputFields)
	}
	request.Input = input

	createdAt := request.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	actionRequest := connectors.ActionRequest{
		Source:     request.Source,
		Target:     resolved.Target,
		Profile:    resolved.Profile,
		ActionName: request.ActionName,
		Input:      request.Input,
		Reason:     request.Reason,
		CreatedAt:  createdAt,
	}

	prepared, err := connector.PrepareAction(ctx, actionRequest)
	if err != nil {
		return PreparedRequest{}, redactSensitivePreparationError(err, request.Input, actionDefinition.SensitiveInputFields)
	}
	if err := validatePreparedAction(prepared, resolved, actionDefinition, request); err != nil {
		return PreparedRequest{}, err
	}
	if prepared.RetryPolicy != nil {
		actionDefinition.RetryPolicy = *prepared.RetryPolicy
		if err := connectors.ValidateActionDefinitions([]connectors.ActionDefinition{actionDefinition}, resolved.Target.ConnectorKind+" prepared action"); err != nil {
			return PreparedRequest{}, fmt.Errorf("invalid prepared retry policy: %w", err)
		}
		for _, field := range prepared.RetryPolicy.PreconditionFields {
			if !preparedRetryPreconditionPresent(prepared.Payload, field) {
				return PreparedRequest{}, fmt.Errorf("invalid prepared retry policy: precondition field %q has no concrete execution value", field)
			}
		}
	}
	dependencies, err := s.resolveApprovalDependencies(ctx, resolved.Target, prepared.Dependencies)
	if err != nil {
		return PreparedRequest{}, err
	}

	return PreparedRequest{
		Target:           resolved.Target,
		Profile:          resolved.Profile,
		ConnectorVersion: connector.Version(),
		ActionDefinition: actionDefinition,
		Action:           prepared,
		Requested:        request,
		IdempotencyInput: idempotencyInput,
		Dependencies:     dependencies,
	}, nil
}

func redactSensitivePreparationError(err error, input map[string]any, sensitiveFields []string) error {
	if err == nil || len(sensitiveFields) == 0 {
		return err
	}
	message := err.Error()
	allVariants := make([]string, 0, len(sensitiveFields))
	for _, field := range sensitiveFields {
		value, ok := input[field]
		if !ok || value == nil {
			continue
		}
		variants, collectErr := sensitivePreparationVariants(value)
		if collectErr != nil {
			return preserveConnectorErrorClassification(err, "connector action preparation failed while validating sensitive input", nil)
		}
		allVariants = append(allVariants, variants...)
		for _, variant := range variants {
			if variant == "" || variant == "null" || !strings.Contains(message, variant) {
				continue
			}
			if len(variant) < 4 {
				message = "connector action preparation failed while validating sensitive input"
				break
			}
			message = strings.ReplaceAll(message, variant, "[REDACTED SENSITIVE INPUT]")
		}
	}
	return preserveConnectorErrorClassification(err, message, redactSensitiveErrorDetails(connectors.ErrorDetails(err), allVariants))
}

func sensitivePreparationVariants(value any) ([]string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	unique := map[string]struct{}{}
	var collect func(any)
	collect = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for _, nested := range typed {
				collect(nested)
			}
		case []any:
			for _, nested := range typed {
				collect(nested)
			}
		case nil:
		default:
			unique[fmt.Sprint(typed)] = struct{}{}
		}
	}
	collect(value)
	unique[fmt.Sprint(value)] = struct{}{}
	unique[string(encoded)] = struct{}{}
	variants := make([]string, 0, len(unique))
	for variant := range unique {
		variants = append(variants, variant)
	}
	sort.Slice(variants, func(i, j int) bool { return len(variants[i]) > len(variants[j]) })
	return variants, nil
}

func preserveConnectorErrorClassification(source error, message string, details map[string]any) error {
	redacted := errors.New(message)
	if code := connectors.ErrorCode(source); code != "" {
		return connectors.ClassifyActionError(code, connectors.ErrorStatus(source), details, redacted)
	}
	return redacted
}

func redactSensitiveErrorDetails(details map[string]any, variants []string) map[string]any {
	if len(details) == 0 {
		return nil
	}
	redactString := func(value string) string {
		for _, variant := range variants {
			if variant == "" || variant == "null" || !strings.Contains(value, variant) {
				continue
			}
			if len(variant) < 4 {
				return "[REDACTED SENSITIVE INPUT]"
			}
			value = strings.ReplaceAll(value, variant, "[REDACTED SENSITIVE INPUT]")
		}
		return value
	}
	isSensitiveScalar := func(value any) bool {
		text := fmt.Sprint(value)
		for _, variant := range variants {
			if variant != "" && variant != "null" && text == variant {
				return true
			}
		}
		return false
	}
	var redact func(any) any
	redact = func(value any) any {
		switch typed := value.(type) {
		case string:
			return redactString(typed)
		case map[string]any:
			result := make(map[string]any, len(typed))
			for key, nested := range typed {
				result[redactString(key)] = redact(nested)
			}
			return result
		case []any:
			result := make([]any, len(typed))
			for index, nested := range typed {
				result[index] = redact(nested)
			}
			return result
		default:
			if isSensitiveScalar(typed) {
				return "[REDACTED SENSITIVE INPUT]"
			}
			return typed
		}
	}
	redacted, _ := redact(details).(map[string]any)
	return redacted
}

func preparedRetryPreconditionPresent(payload map[string]any, field string) bool {
	value, ok := payload[field]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) != 0
	case map[string]any:
		return len(typed) != 0
	default:
		return true
	}
}

func (s *Service) resolveApprovalDependencies(ctx context.Context, target connectors.TargetView, declared []connectors.ApprovalDependency) ([]ResolvedDependency, error) {
	dependencies := append([]connectors.ApprovalDependency(nil), declared...)
	for index := range dependencies {
		dependencies[index].TargetRef = strings.TrimSpace(dependencies[index].TargetRef)
		dependencies[index].Purpose = strings.TrimSpace(dependencies[index].Purpose)
	}
	sort.Slice(dependencies, func(i, j int) bool {
		if dependencies[i].Purpose != dependencies[j].Purpose {
			return dependencies[i].Purpose < dependencies[j].Purpose
		}
		return dependencies[i].TargetRef < dependencies[j].TargetRef
	})
	seen := map[string]bool{}
	resolved := make([]ResolvedDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency.TargetRef == "" || !connectors.ValidIdentifier(dependency.Purpose) {
			return nil, fmt.Errorf("prepared action has an invalid approval dependency")
		}
		key := dependency.Purpose + "\x00" + dependency.TargetRef
		if seen[key] {
			continue
		}
		seen[key] = true
		item, err := s.targets.ResolveActionTarget(ctx, dependency.TargetRef)
		if err != nil {
			return nil, fmt.Errorf("resolve %s approval dependency: %w", dependency.Purpose, err)
		}
		if target.ProjectID > 0 && item.Target.ProjectID != target.ProjectID {
			return nil, fmt.Errorf("%s approval dependency must belong to the same project", dependency.Purpose)
		}
		resolved = append(resolved, ResolvedDependency{Purpose: dependency.Purpose, Target: item.Target, Profile: item.Profile})
	}
	return resolved, nil
}

func stringMapValue(values map[string]any, key string) string {
	if values == nil || values[key] == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(values[key]))
}

func connectorActionDefinition(ctx context.Context, connector connectors.Connector, target connectors.TargetView, profile connectors.CredentialProfileView, actionName string) (connectors.ActionDefinition, error) {
	actions, err := connectors.GetActionDefinitions(ctx, connector, target, profile)
	if err != nil {
		return connectors.ActionDefinition{}, err
	}
	for _, action := range actions {
		if action.Name == actionName {
			return action, nil
		}
	}
	return connectors.ActionDefinition{}, fmt.Errorf("unsupported connector action %q", actionName)
}

func validatePreparedAction(prepared connectors.PreparedAction, resolved ResolvedTarget, definition connectors.ActionDefinition, request PrepareRequest) error {
	if prepared.ConnectorKind != resolved.Target.ConnectorKind {
		return fmt.Errorf("prepared action connector kind drifted from %q to %q", resolved.Target.ConnectorKind, prepared.ConnectorKind)
	}
	if prepared.TargetRef != resolved.Target.Ref {
		return fmt.Errorf("prepared action target ref drifted from %q to %q", resolved.Target.Ref, prepared.TargetRef)
	}
	if prepared.ProfileID != resolved.Profile.ID {
		return fmt.Errorf("prepared action profile id drifted from %d to %d", resolved.Profile.ID, prepared.ProfileID)
	}
	if prepared.ActionName != request.ActionName {
		return fmt.Errorf("prepared action name drifted from %q to %q", request.ActionName, prepared.ActionName)
	}
	if prepared.Risk != definition.Risk {
		return fmt.Errorf("prepared action risk drifted from %q to %q", definition.Risk, prepared.Risk)
	}
	if field, ok := secretPayloadField(prepared.Payload); ok {
		return fmt.Errorf("prepared action payload field %q must not contain secrets; store secrets in credential profiles instead", field)
	}
	if field, ok := secretPayloadField(prepared.Preview); ok {
		return fmt.Errorf("prepared action preview field %q must not contain credentials; show only intentional action content", field)
	}
	if field, ok := secretPayloadField(prepared.ContextMaterial); ok {
		return fmt.Errorf("prepared action context field %q must not contain credentials; bind only non-secret execution context", field)
	}
	previewJSON, err := json.Marshal(prepared.Preview)
	if err != nil {
		return fmt.Errorf("prepared action preview must be valid JSON: %w", err)
	}
	if len(previewJSON) > maxPreparedActionPreviewBytes {
		return fmt.Errorf("prepared action preview exceeds %d bytes", maxPreparedActionPreviewBytes)
	}
	contextJSON, err := json.Marshal(prepared.ContextMaterial)
	if err != nil {
		return fmt.Errorf("prepared action context must be valid JSON: %w", err)
	}
	if len(contextJSON) > maxPreparedActionPreviewBytes {
		return fmt.Errorf("prepared action context exceeds %d bytes", maxPreparedActionPreviewBytes)
	}
	return nil
}

func secretPayloadField(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if looksLikeSecretField(key) {
				return key, true
			}
			if field, ok := secretPayloadField(nested); ok {
				return field, true
			}
		}
	case []any:
		for _, nested := range typed {
			if field, ok := secretPayloadField(nested); ok {
				return field, true
			}
		}
	case []map[string]any:
		for _, nested := range typed {
			if field, ok := secretPayloadField(nested); ok {
				return field, true
			}
		}
	}
	return "", false
}

func looksLikeSecretField(key string) bool {
	parts := secretFieldParts(key)
	compact := strings.Join(parts, "")
	if compact == "" {
		return false
	}
	exactSecrets := map[string]bool{
		"apikey":           true,
		"authorization":    true,
		"bearer":           true,
		"credential":       true,
		"credentialhash":   true,
		"credentialsecret": true,
		"credentialvalue":  true,
		"password":         true,
		"passwd":           true,
		"privatekey":       true,
		"secret":           true,
		"token":            true,
	}
	if exactSecrets[compact] {
		return true
	}
	if strings.HasSuffix(compact, "apikey") || strings.HasSuffix(compact, "privatekey") {
		return true
	}
	secretParts := map[string]bool{
		"authorization": true,
		"bearer":        true,
		"password":      true,
		"passwd":        true,
		"secret":        true,
		"token":         true,
	}
	for index, part := range parts {
		if secretParts[part] {
			return true
		}
		if index+1 < len(parts) && ((part == "api" && parts[index+1] == "key") || (part == "private" && parts[index+1] == "key")) {
			return true
		}
		if part == "credential" && index+1 < len(parts) {
			switch parts[index+1] {
			case "hash", "secret", "value":
				return true
			}
		}
	}
	return false
}

func secretFieldParts(key string) []string {
	fields := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(key)), func(r rune) bool {
		switch r {
		case '-', '_', ' ', '.', '/', ':':
			return true
		default:
			return false
		}
	})
	parts := fields[:0]
	for _, field := range fields {
		if field != "" {
			parts = append(parts, field)
		}
	}
	return parts
}
