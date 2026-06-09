package actions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type fakeResolver struct {
	target  connectors.TargetView
	profile connectors.CredentialProfileView
	err     error
	seenRef string
}

func (r *fakeResolver) ResolveActionTarget(_ context.Context, targetRef string) (ResolvedTarget, error) {
	r.seenRef = targetRef
	if r.err != nil {
		return ResolvedTarget{}, r.err
	}
	return ResolvedTarget{Target: r.target, Profile: r.profile}, nil
}

type prepareConnector struct {
	kind string
	seen connectors.ActionRequest
	err  error
}

func (c *prepareConnector) Kind() string                    { return c.kind }
func (c *prepareConnector) Label() string                   { return "Test" }
func (c *prepareConnector) Version() string                 { return "0.1" }
func (c *prepareConnector) TargetSchema() connectors.Schema { return connectors.Schema{} }
func (c *prepareConnector) CredentialSchemas() []connectors.CredentialSchema {
	return nil
}
func (c *prepareConnector) GetHelp(context.Context, connectors.TargetView) (connectors.ConnectorHelp, error) {
	return connectors.ConnectorHelp{}, nil
}
func (c *prepareConnector) GetActionList(context.Context, connectors.TargetView) ([]connectors.ActionDefinition, error) {
	return nil, nil
}
func (c *prepareConnector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	c.seen = req
	if c.err != nil {
		return connectors.PreparedAction{}, c.err
	}
	return connectors.PreparedAction{
		ConnectorKind: c.kind,
		TargetRef:     req.Target.Ref,
		ProfileID:     req.Profile.ID,
		ActionName:    req.ActionName,
		Risk:          connectors.RiskRead,
		Title:         "Prepared",
	}, nil
}
func (c *prepareConnector) ExecuteAction(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error) {
	return connectors.ActionResult{}, nil
}

func TestServicePrepareResolvesTargetAndConnector(t *testing.T) {
	registry := connectors.NewRegistry()
	connector := &prepareConnector{kind: "postgres"}
	if err := registry.Register(connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}

	resolver := &fakeResolver{
		target: connectors.TargetView{
			ID:            7,
			Ref:           "postgres:main",
			ConnectorKind: "postgres",
			Name:          "Main DB",
		},
		profile: connectors.CredentialProfileView{
			ID:     11,
			Kind:   "password",
			Label:  "readonly",
			Public: map[string]any{"username": "app_readonly"},
		},
	}
	service := NewService(registry, resolver)
	createdAt := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)

	prepared, err := service.Prepare(context.Background(), PrepareRequest{
		Source:     "mcp",
		TargetRef:  "postgres:main",
		ActionName: "query_readonly",
		Input:      map[string]any{"sql": "select 1"},
		Reason:     "smoke",
		CreatedAt:  createdAt,
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if resolver.seenRef != "postgres:main" {
		t.Fatalf("resolver saw ref %q", resolver.seenRef)
	}
	if connector.seen.ActionName != "query_readonly" {
		t.Fatalf("connector saw action %q", connector.seen.ActionName)
	}
	if connector.seen.Profile.Label != "readonly" {
		t.Fatalf("connector saw profile %#v", connector.seen.Profile)
	}
	if !connector.seen.CreatedAt.Equal(createdAt) {
		t.Fatalf("created_at = %s", connector.seen.CreatedAt)
	}
	if prepared.Action.ProfileID != 11 || prepared.Action.TargetRef != "postgres:main" {
		t.Fatalf("prepared action mismatch: %#v", prepared.Action)
	}
}

func TestServicePrepareRejectsInvalidInput(t *testing.T) {
	service := NewService(connectors.NewRegistry(), &fakeResolver{})

	if _, err := service.Prepare(context.Background(), PrepareRequest{ActionName: "query_readonly"}); err == nil {
		t.Fatal("expected missing target_ref error")
	}
	if _, err := service.Prepare(context.Background(), PrepareRequest{TargetRef: "postgres:main", ActionName: "bad-action"}); err == nil {
		t.Fatal("expected invalid action name error")
	}
}

func TestServicePrepareReturnsConnectorUnavailable(t *testing.T) {
	resolver := &fakeResolver{
		target: connectors.TargetView{
			Ref:           "redis:cache",
			ConnectorKind: "redis",
		},
	}
	service := NewService(connectors.NewRegistry(), resolver)

	_, err := service.Prepare(context.Background(), PrepareRequest{
		TargetRef:  "redis:cache",
		ActionName: "get_key",
	})
	if !errors.Is(err, ErrConnectorUnavailable) {
		t.Fatalf("expected ErrConnectorUnavailable, got %v", err)
	}
}

func TestServicePreparePropagatesResolverError(t *testing.T) {
	want := errors.New("resolver failed")
	service := NewService(connectors.NewRegistry(), &fakeResolver{err: want})

	_, err := service.Prepare(context.Background(), PrepareRequest{
		TargetRef:  "postgres:main",
		ActionName: "get_tables",
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected resolver error, got %v", err)
	}
}
