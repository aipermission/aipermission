package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

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
	input, err := connectors.NormalizeSchemaValues(actionDefinition.InputSchema, request.Input)
	if err != nil {
		return PreparedRequest{}, err
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
		return PreparedRequest{}, err
	}
	if err := validatePreparedAction(prepared, resolved, actionDefinition, request); err != nil {
		return PreparedRequest{}, err
	}

	return PreparedRequest{
		Target:           resolved.Target,
		Profile:          resolved.Profile,
		ConnectorVersion: connector.Version(),
		ActionDefinition: actionDefinition,
		Action:           prepared,
		Requested:        request,
	}, nil
}

func connectorActionDefinition(ctx context.Context, connector connectors.Connector, target connectors.TargetView, profile connectors.CredentialProfileView, actionName string) (connectors.ActionDefinition, error) {
	actions, err := connector.GetActionList(ctx, target, profile)
	if err != nil {
		return connectors.ActionDefinition{}, err
	}
	if err := connectors.ValidateActionDefinitions(actions, connector.Kind()+" actions"); err != nil {
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
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(key))
	for _, marker := range []string{"password", "passwd", "token", "secret", "apikey", "privatekey", "authorization", "bearer", "credential"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
