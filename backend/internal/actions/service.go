package actions

import (
	"context"
	"errors"
	"fmt"
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
// A future resolver can map refs like "ssh:12" or "postgres:main-readonly"
// without making numeric IDs ambiguous.
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
	Target    connectors.TargetView
	Profile   connectors.CredentialProfileView
	Action    connectors.PreparedAction
	Requested PrepareRequest
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

	return PreparedRequest{
		Target:    resolved.Target,
		Profile:   resolved.Profile,
		Action:    prepared,
		Requested: request,
	}, nil
}
