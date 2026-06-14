package actions

import (
	"context"
	"errors"
	"strings"
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
	kind     string
	actions  []connectors.ActionDefinition
	prepared *connectors.PreparedAction
	seen     connectors.ActionRequest
	err      error
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
func (c *prepareConnector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	if c.actions != nil {
		return c.actions, nil
	}
	return []connectors.ActionDefinition{
		{
			Name:        "query_readonly",
			Description: "Run a bounded read-only query.",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "sql", Label: "SQL", Type: connectors.FieldString, Required: true},
			}},
		},
	}, nil
}
func (c *prepareConnector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	c.seen = req
	if c.err != nil {
		return connectors.PreparedAction{}, c.err
	}
	if c.prepared != nil {
		return *c.prepared, nil
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

func TestServicePrepareUsesRegisteredThirdConnector(t *testing.T) {
	registry := connectors.NewRegistry()
	connector := &prepareConnector{
		kind: "memory",
		actions: []connectors.ActionDefinition{
			{
				Name:        "get_value",
				Description: "Read one in-memory value.",
				Risk:        connectors.RiskRead,
				InputSchema: connectors.Schema{Fields: []connectors.Field{
					{Name: "key", Label: "Key", Type: connectors.FieldString, Required: true},
				}},
			},
		},
	}
	if err := registry.Register(connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	service := NewService(registry, &fakeResolver{
		target: connectors.TargetView{
			ID:            21,
			Ref:           "memory:21:34",
			ConnectorKind: "memory",
			Name:          "Session cache",
		},
		profile: connectors.CredentialProfileView{
			ID:            34,
			TargetID:      21,
			ConnectorKind: "memory",
			Kind:          "local",
			Label:         "readonly",
		},
	})

	prepared, err := service.Prepare(context.Background(), PrepareRequest{
		TargetRef:  "memory:21:34",
		ActionName: "get_value",
		Input:      map[string]any{"key": "release"},
		Reason:     "prove generic connector path",
	})
	if err != nil {
		t.Fatalf("prepare third connector: %v", err)
	}
	if prepared.Target.ConnectorKind != "memory" || prepared.ActionDefinition.Name != "get_value" {
		t.Fatalf("unexpected prepared connector action: %#v", prepared)
	}
	if connector.seen.Target.ConnectorKind != "memory" || connector.seen.Profile.Label != "readonly" {
		t.Fatalf("connector did not receive resolved target/profile: %#v", connector.seen)
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

func TestRegistryRejectsSecretActionInputSchemaBeforePrepare(t *testing.T) {
	registry := connectors.NewRegistry()
	connector := &prepareConnector{
		kind: "api",
		actions: []connectors.ActionDefinition{
			{
				Name: "call_action",
				Risk: connectors.RiskRead,
				InputSchema: connectors.Schema{Fields: []connectors.Field{
					{Name: "api_key", Label: "API key", Type: connectors.FieldSecret, Secret: true},
				}},
			},
		},
	}
	err := registry.Register(connector)
	if err == nil || !strings.Contains(err.Error(), "store secrets in credential profiles") {
		t.Fatalf("expected secret action input schema rejection at registration, got %v", err)
	}
	if connector.seen.ActionName != "" {
		t.Fatalf("connector PrepareAction should not run during registration rejection: %#v", connector.seen)
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

func TestServicePrepareRejectsPreparedActionDrift(t *testing.T) {
	tests := []struct {
		name     string
		prepared connectors.PreparedAction
		want     string
	}{
		{
			name: "connector kind",
			prepared: connectors.PreparedAction{
				ConnectorKind: "redis",
				TargetRef:     "postgres:1:2",
				ProfileID:     2,
				ActionName:    "query_readonly",
				Risk:          connectors.RiskRead,
			},
			want: "connector kind drifted",
		},
		{
			name: "target ref",
			prepared: connectors.PreparedAction{
				ConnectorKind: "postgres",
				TargetRef:     "postgres:9:2",
				ProfileID:     2,
				ActionName:    "query_readonly",
				Risk:          connectors.RiskRead,
			},
			want: "target ref drifted",
		},
		{
			name: "profile id",
			prepared: connectors.PreparedAction{
				ConnectorKind: "postgres",
				TargetRef:     "postgres:1:2",
				ProfileID:     99,
				ActionName:    "query_readonly",
				Risk:          connectors.RiskRead,
			},
			want: "profile id drifted",
		},
		{
			name: "action name",
			prepared: connectors.PreparedAction{
				ConnectorKind: "postgres",
				TargetRef:     "postgres:1:2",
				ProfileID:     2,
				ActionName:    "drop_database",
				Risk:          connectors.RiskRead,
			},
			want: "action name drifted",
		},
		{
			name: "risk",
			prepared: connectors.PreparedAction{
				ConnectorKind: "postgres",
				TargetRef:     "postgres:1:2",
				ProfileID:     2,
				ActionName:    "query_readonly",
				Risk:          connectors.RiskDestructive,
			},
			want: "risk drifted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := connectors.NewRegistry()
			prepared := tt.prepared
			connector := &prepareConnector{kind: "postgres", prepared: &prepared}
			if err := registry.Register(connector); err != nil {
				t.Fatalf("register connector: %v", err)
			}
			service := NewService(registry, &fakeResolver{
				target: connectors.TargetView{
					ID:            1,
					Ref:           "postgres:1:2",
					ConnectorKind: "postgres",
					Name:          "Main DB",
				},
				profile: connectors.CredentialProfileView{
					ID:            2,
					TargetID:      1,
					ConnectorKind: "postgres",
					Kind:          "username_password",
					Label:         "readonly",
				},
			})
			_, err := service.Prepare(context.Background(), PrepareRequest{
				TargetRef:  "postgres:1:2",
				ActionName: "query_readonly",
				Input:      map[string]any{"sql": "select 1"},
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}
