package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type fakeResolver struct {
	target  connectors.TargetView
	profile connectors.CredentialProfileView
	refs    map[string]ResolvedTarget
	err     error
	seenRef string
}

func (r *fakeResolver) ResolveActionTarget(_ context.Context, targetRef string) (ResolvedTarget, error) {
	r.seenRef = targetRef
	if r.err != nil {
		return ResolvedTarget{}, r.err
	}
	if resolved, ok := r.refs[targetRef]; ok {
		return resolved, nil
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
			Label:       "Query read-only",
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
		Dependencies:  connectors.NetworkTransportDependencies(req.Target),
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

func TestServicePrepareResolvesOverSSHApprovalDependency(t *testing.T) {
	registry := connectors.NewRegistry()
	connector := &prepareConnector{kind: "memory"}
	if err := registry.Register(connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	resolver := &fakeResolver{refs: map[string]ResolvedTarget{
		"memory:21:34": {
			Target: connectors.TargetView{
				ID:            21,
				ProjectID:     8,
				Ref:           "memory:21:34",
				ConnectorKind: "memory",
				Config: map[string]any{
					"connection_mode":      "over_ssh",
					"transport_target_ref": "ssh:7:11",
				},
			},
			Profile: connectors.CredentialProfileView{ID: 34, TargetID: 21, ConnectorKind: "memory", Kind: "local"},
		},
		"ssh:7:11": {
			Target:  connectors.TargetView{ID: 7, ProjectID: 8, Ref: "ssh:7:11", ConnectorKind: "ssh", UpdatedAt: "2026-08-03T10:00:00Z"},
			Profile: connectors.CredentialProfileView{ID: 11, TargetID: 7, ConnectorKind: "ssh", Kind: "private_key", SecretRevision: "ssh-secret-a"},
		},
	}}

	prepared, err := NewService(registry, resolver).Prepare(context.Background(), PrepareRequest{
		TargetRef:  "memory:21:34",
		ActionName: "query_readonly",
		Input:      map[string]any{"sql": "select 1"},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(prepared.Dependencies) != 1 {
		t.Fatalf("dependencies = %#v", prepared.Dependencies)
	}
	dependency := prepared.Dependencies[0]
	if dependency.Purpose != "network_transport" || dependency.Target.Ref != "ssh:7:11" || dependency.Profile.SecretRevision != "ssh-secret-a" {
		t.Fatalf("dependency = %#v", dependency)
	}
}

func TestServicePrepareResolvesCommandTransportApprovalDependency(t *testing.T) {
	registry := connectors.NewRegistry()
	connector := &prepareConnector{
		kind: "memory",
		prepared: &connectors.PreparedAction{
			ConnectorKind: "memory",
			TargetRef:     "memory:21:34",
			ProfileID:     34,
			ActionName:    "query_readonly",
			Risk:          connectors.RiskRead,
			Dependencies: []connectors.ApprovalDependency{{
				TargetRef: "ssh:7:11",
				Purpose:   connectors.CommandTransportCapabilityName,
			}},
		},
	}
	if err := registry.Register(connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	resolver := &fakeResolver{refs: map[string]ResolvedTarget{
		"memory:21:34": {
			Target:  connectors.TargetView{ID: 21, ProjectID: 8, Ref: "memory:21:34", ConnectorKind: "memory"},
			Profile: connectors.CredentialProfileView{ID: 34, TargetID: 21, ConnectorKind: "memory", Kind: "local"},
		},
		"ssh:7:11": {
			Target:  connectors.TargetView{ID: 7, ProjectID: 8, Ref: "ssh:7:11", ConnectorKind: "ssh"},
			Profile: connectors.CredentialProfileView{ID: 11, TargetID: 7, ConnectorKind: "ssh", Kind: "private_key"},
		},
	}}

	prepared, err := NewService(registry, resolver).Prepare(t.Context(), PrepareRequest{
		TargetRef: "memory:21:34", ActionName: "query_readonly", Input: map[string]any{"sql": "select 1"},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(prepared.Dependencies) != 1 || prepared.Dependencies[0].Purpose != connectors.CommandTransportCapabilityName {
		t.Fatalf("command transport dependencies = %#v", prepared.Dependencies)
	}
}

func TestServicePrepareRejectsCrossProjectApprovalDependency(t *testing.T) {
	registry := connectors.NewRegistry()
	connector := &prepareConnector{kind: "memory"}
	if err := registry.Register(connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	resolver := &fakeResolver{refs: map[string]ResolvedTarget{
		"memory:21:34": {
			Target: connectors.TargetView{
				ID: 21, ProjectID: 8, Ref: "memory:21:34", ConnectorKind: "memory",
				Config: map[string]any{"connection_mode": "over_ssh", "transport_target_ref": "ssh:7:11"},
			},
			Profile: connectors.CredentialProfileView{ID: 34, TargetID: 21},
		},
		"ssh:7:11": {
			Target:  connectors.TargetView{ID: 7, ProjectID: 9, Ref: "ssh:7:11", ConnectorKind: "ssh"},
			Profile: connectors.CredentialProfileView{ID: 11, TargetID: 7},
		},
	}}

	_, err := NewService(registry, resolver).Prepare(context.Background(), PrepareRequest{
		TargetRef:  "memory:21:34",
		ActionName: "query_readonly",
		Input:      map[string]any{"sql": "select 1"},
	})
	if err == nil || !strings.Contains(err.Error(), "same project") {
		t.Fatalf("expected cross-project dependency rejection, got %v", err)
	}
}

func TestServicePrepareUsesRegisteredThirdConnector(t *testing.T) {
	registry := connectors.NewRegistry()
	connector := &prepareConnector{
		kind: "memory",
		actions: []connectors.ActionDefinition{
			{
				Name:        "get_value",
				Label:       "Get value",
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

func TestServicePrepareRedactsSensitiveInputFromConnectorErrors(t *testing.T) {
	registry := connectors.NewRegistry()
	connector := &prepareConnector{
		kind: "memory",
		err:  errors.New("rejected payload=x"),
		actions: []connectors.ActionDefinition{{
			Name: "query_readonly", Label: "Query", Description: "Query.", Risk: connectors.RiskRead,
			InputSchema:          connectors.Schema{Fields: []connectors.Field{{Name: "payload", Label: "Payload", Type: connectors.FieldString, Required: true}}},
			SensitiveInputFields: []string{"payload"},
		}},
	}
	if err := registry.Register(connector); err != nil {
		t.Fatal(err)
	}
	service := NewService(registry, &fakeResolver{
		target:  connectors.TargetView{ID: 21, Ref: "memory:21:34", ConnectorKind: "memory"},
		profile: connectors.CredentialProfileView{ID: 34, TargetID: 21, ConnectorKind: "memory"},
	})
	_, err := service.Prepare(t.Context(), PrepareRequest{
		TargetRef: "memory:21:34", ActionName: "query_readonly", Input: map[string]any{"payload": "x"},
	})
	if err == nil || strings.Contains(err.Error(), "payload=x") {
		t.Fatalf("sensitive preparation error was not redacted: %v", err)
	}
}

func TestServicePreparePreservesClassifiedSensitiveInputError(t *testing.T) {
	registry := connectors.NewRegistry()
	connector := &prepareConnector{
		kind: "memory",
		err: connectors.ClassifyActionError(
			"payload_rejected", connectors.ResultFailed, map[string]any{
				"retry_safe": false,
				"diagnostic": map[string]any{"rejected_value": "secret-value", "context": []any{"prefix-secret-value-suffix"}},
			}, errors.New("rejected payload=secret-value"),
		),
		actions: []connectors.ActionDefinition{{
			Name: "query_readonly", Label: "Query", Description: "Query.", Risk: connectors.RiskRead,
			InputSchema:          connectors.Schema{Fields: []connectors.Field{{Name: "payload", Label: "Payload", Type: connectors.FieldString, Required: true}}},
			SensitiveInputFields: []string{"payload"},
		}},
	}
	if err := registry.Register(connector); err != nil {
		t.Fatal(err)
	}
	service := NewService(registry, &fakeResolver{
		target:  connectors.TargetView{ID: 21, Ref: "memory:21:34", ConnectorKind: "memory"},
		profile: connectors.CredentialProfileView{ID: 34, TargetID: 21, ConnectorKind: "memory"},
	})
	_, err := service.Prepare(t.Context(), PrepareRequest{
		TargetRef: "memory:21:34", ActionName: "query_readonly", Input: map[string]any{"payload": "secret-value"},
	})
	if err == nil || connectors.ErrorCode(err) != "payload_rejected" || connectors.ErrorStatus(err) != connectors.ResultFailed {
		t.Fatalf("classification was not preserved: %v", err)
	}
	if strings.Contains(err.Error(), "secret-value") || !strings.Contains(err.Error(), "[REDACTED SENSITIVE INPUT]") {
		t.Fatalf("classified error was not redacted: %v", err)
	}
	detailsJSON, marshalErr := json.Marshal(connectors.ErrorDetails(err))
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(detailsJSON), "secret-value") || !strings.Contains(string(detailsJSON), "[REDACTED SENSITIVE INPUT]") {
		t.Fatalf("classified error details were not redacted: %s", detailsJSON)
	}
}

func TestServicePrepareRedactsShortSensitiveValuesFromClassifiedErrorDetails(t *testing.T) {
	registry := connectors.NewRegistry()
	connector := &prepareConnector{
		kind: "memory",
		err: connectors.ClassifyActionError(
			"payload_rejected", connectors.ResultFailed, map[string]any{"diagnostic": "value x was rejected"}, errors.New("rejected payload=x"),
		),
		actions: []connectors.ActionDefinition{{
			Name: "query_readonly", Label: "Query", Description: "Query.", Risk: connectors.RiskRead,
			InputSchema:          connectors.Schema{Fields: []connectors.Field{{Name: "payload", Label: "Payload", Type: connectors.FieldString, Required: true}}},
			SensitiveInputFields: []string{"payload"},
		}},
	}
	if err := registry.Register(connector); err != nil {
		t.Fatal(err)
	}
	service := NewService(registry, &fakeResolver{
		target:  connectors.TargetView{ID: 21, Ref: "memory:21:34", ConnectorKind: "memory"},
		profile: connectors.CredentialProfileView{ID: 34, TargetID: 21, ConnectorKind: "memory"},
	})
	_, err := service.Prepare(t.Context(), PrepareRequest{
		TargetRef: "memory:21:34", ActionName: "query_readonly", Input: map[string]any{"payload": "x"},
	})
	if err == nil || strings.Contains(err.Error(), "payload=x") {
		t.Fatalf("short sensitive preparation error was not redacted: %v", err)
	}
	detailsJSON, marshalErr := json.Marshal(connectors.ErrorDetails(err))
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(detailsJSON), "value x") || !strings.Contains(string(detailsJSON), "[REDACTED SENSITIVE INPUT]") {
		t.Fatalf("short sensitive error details were not redacted: %s", detailsJSON)
	}
}

func TestServicePrepareRedactsSensitiveSchemaNormalizationErrors(t *testing.T) {
	registry := connectors.NewRegistry()
	connector := &prepareConnector{
		kind: "memory",
		actions: []connectors.ActionDefinition{{
			Name: "query_readonly", Label: "Query", Description: "Query.", Risk: connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{{
				Name: "payload", Label: "Payload", Type: connectors.FieldSelect, Required: true,
				Options: []connectors.FieldOption{{Value: "allowed", Label: "Allowed"}},
			}}},
			SensitiveInputFields: []string{"payload"},
		}},
	}
	if err := registry.Register(connector); err != nil {
		t.Fatal(err)
	}
	service := NewService(registry, &fakeResolver{
		target:  connectors.TargetView{ID: 21, Ref: "memory:21:34", ConnectorKind: "memory"},
		profile: connectors.CredentialProfileView{ID: 34, TargetID: 21, ConnectorKind: "memory"},
	})
	_, err := service.Prepare(t.Context(), PrepareRequest{
		TargetRef: "memory:21:34", ActionName: "query_readonly", Input: map[string]any{"payload": "secret-option"},
	})
	if err == nil || strings.Contains(err.Error(), "secret-option") || !strings.Contains(err.Error(), "[REDACTED SENSITIVE INPUT]") {
		t.Fatalf("sensitive normalization error was not redacted: %v", err)
	}
}

func TestServicePrepareRejectsOversizedPreview(t *testing.T) {
	registry := connectors.NewRegistry()
	connector := &prepareConnector{
		kind: "memory",
		prepared: &connectors.PreparedAction{
			ConnectorKind: "memory",
			TargetRef:     "memory:21:34",
			ProfileID:     34,
			ActionName:    "query_readonly",
			Risk:          connectors.RiskRead,
			Title:         "Prepared",
			Preview:       map[string]any{"body": strings.Repeat("x", maxPreparedActionPreviewBytes)},
		},
	}
	if err := registry.Register(connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	service := NewService(registry, &fakeResolver{
		target:  connectors.TargetView{ID: 21, Ref: "memory:21:34", ConnectorKind: "memory", Name: "Memory"},
		profile: connectors.CredentialProfileView{ID: 34, TargetID: 21, ConnectorKind: "memory", Kind: "local", Label: "default"},
	})

	_, err := service.Prepare(context.Background(), PrepareRequest{
		TargetRef:  "memory:21:34",
		ActionName: "query_readonly",
		Input:      map[string]any{"sql": "select 1"},
	})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("preview exceeds %d bytes", maxPreparedActionPreviewBytes)) {
		t.Fatalf("expected bounded preview rejection, got %v", err)
	}
}

func TestServicePrepareRejectsSecretOrOversizedContextMaterial(t *testing.T) {
	for name, contextMaterial := range map[string]map[string]any{
		"secret":    {"api_token": "do-not-store"},
		"oversized": {"metadata": strings.Repeat("x", maxPreparedActionPreviewBytes)},
	} {
		t.Run(name, func(t *testing.T) {
			registry := connectors.NewRegistry()
			connector := &prepareConnector{
				kind: "memory",
				prepared: &connectors.PreparedAction{
					ConnectorKind:   "memory",
					TargetRef:       "memory:21:34",
					ProfileID:       34,
					ActionName:      "query_readonly",
					Risk:            connectors.RiskRead,
					ContextMaterial: contextMaterial,
				},
			}
			if err := registry.Register(connector); err != nil {
				t.Fatalf("register connector: %v", err)
			}
			service := NewService(registry, &fakeResolver{
				target:  connectors.TargetView{ID: 21, Ref: "memory:21:34", ConnectorKind: "memory"},
				profile: connectors.CredentialProfileView{ID: 34, TargetID: 21},
			})
			_, err := service.Prepare(context.Background(), PrepareRequest{
				TargetRef: "memory:21:34", ActionName: "query_readonly", Input: map[string]any{"sql": "select 1"},
			})
			if err == nil || (!strings.Contains(err.Error(), "must not contain credentials") && !strings.Contains(err.Error(), "context exceeds")) {
				t.Fatalf("expected context rejection, got %v", err)
			}
		})
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
				Name:        "call_action",
				Label:       "Call action",
				Description: "Call a test action.",
				Risk:        connectors.RiskRead,
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

func TestServicePrepareAppliesAndValidatesInputSpecificRetryPolicy(t *testing.T) {
	registry := connectors.NewRegistry()
	prepared := connectors.PreparedAction{
		ConnectorKind: "postgres",
		TargetRef:     "postgres:1:2",
		ProfileID:     2,
		ActionName:    "guarded_write",
		Risk:          connectors.RiskWrite,
		Title:         "Prepared",
		Payload:       map[string]any{"expected_revision": "revision-1"},
		RetryPolicy:   connectors.ConditionalRetryPolicy("expected_revision"),
	}
	connector := &prepareConnector{
		kind:     "postgres",
		prepared: &prepared,
		actions: []connectors.ActionDefinition{{
			Name: "guarded_write", Label: "Guarded write", Description: "Run a guarded mutation.", Risk: connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "expected_revision", Label: "Expected revision", Type: connectors.FieldString, Required: true}}},
		}},
	}
	if err := registry.Register(connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	service := NewService(registry, &fakeResolver{
		target:  connectors.TargetView{ID: 1, Ref: "postgres:1:2", ConnectorKind: "postgres"},
		profile: connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: "postgres"},
	})

	result, err := service.Prepare(context.Background(), PrepareRequest{
		TargetRef: "postgres:1:2", ActionName: "guarded_write", Input: map[string]any{"expected_revision": "revision-1"},
	})
	if err != nil {
		t.Fatalf("prepare conditional request: %v", err)
	}
	if result.ActionDefinition.RetryPolicy.Class != connectors.RetryConditional {
		t.Fatalf("retry policy = %#v", result.ActionDefinition.RetryPolicy)
	}

	prepared.RetryPolicy = connectors.ConditionalRetryPolicy("missing")
	connector.prepared = &prepared
	if _, err := service.Prepare(context.Background(), PrepareRequest{
		TargetRef: "postgres:1:2", ActionName: "guarded_write", Input: map[string]any{"expected_revision": "revision-1"},
	}); err == nil || !strings.Contains(err.Error(), "invalid prepared retry policy") {
		t.Fatalf("invalid prepared retry policy error = %v", err)
	}

	prepared.RetryPolicy = connectors.ConditionalRetryPolicy("expected_revision")
	prepared.Payload = map[string]any{"expected_revision": ""}
	connector.prepared = &prepared
	if _, err := service.Prepare(context.Background(), PrepareRequest{
		TargetRef: "postgres:1:2", ActionName: "guarded_write", Input: map[string]any{"expected_revision": "revision-1"},
	}); err == nil || !strings.Contains(err.Error(), "no concrete execution value") {
		t.Fatalf("empty prepared precondition error = %v", err)
	}
}

func TestServicePrepareRejectsSecretLikePreparedPayloadFields(t *testing.T) {
	registry := connectors.NewRegistry()
	prepared := connectors.PreparedAction{
		ConnectorKind: "api",
		TargetRef:     "api:1:2",
		ProfileID:     2,
		ActionName:    "call_action",
		Risk:          connectors.RiskRead,
		Payload: map[string]any{
			"request": map[string]any{
				"api_token": "leaked",
			},
		},
	}
	connector := &prepareConnector{
		kind: "api",
		actions: []connectors.ActionDefinition{{
			Name:        "call_action",
			Label:       "Call action",
			Description: "Call a test action.",
			Risk:        connectors.RiskRead,
		}},
		prepared: &prepared,
	}
	if err := registry.Register(connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	service := NewService(registry, &fakeResolver{
		target: connectors.TargetView{
			ID:            1,
			Ref:           "api:1:2",
			ConnectorKind: "api",
			Name:          "Test API",
		},
		profile: connectors.CredentialProfileView{
			ID:            2,
			TargetID:      1,
			ConnectorKind: "api",
			Kind:          "api_key",
			Label:         "default",
		},
	})

	_, err := service.Prepare(context.Background(), PrepareRequest{
		TargetRef:  "api:1:2",
		ActionName: "call_action",
	})
	if err == nil || !strings.Contains(err.Error(), "store secrets in credential profiles") {
		t.Fatalf("expected secret payload field rejection, got %v", err)
	}

	prepared.Payload = nil
	prepared.Preview = map[string]any{"credential_password": "leaked"}
	_, err = service.Prepare(context.Background(), PrepareRequest{
		TargetRef:  "api:1:2",
		ActionName: "call_action",
	})
	if err == nil || !strings.Contains(err.Error(), "preview field") {
		t.Fatalf("expected secret preview field rejection, got %v", err)
	}
}

func TestServicePrepareAllowsNonSecretCredentialReferences(t *testing.T) {
	registry := connectors.NewRegistry()
	prepared := connectors.PreparedAction{
		ConnectorKind: "api",
		TargetRef:     "api:1:2",
		ProfileID:     2,
		ActionName:    "call_action",
		Risk:          connectors.RiskRead,
		Payload: map[string]any{
			"credential_id":   "profile-2",
			"credential_kind": "api_key",
			"credential_ref":  "api:1:2",
		},
	}
	connector := &prepareConnector{
		kind: "api",
		actions: []connectors.ActionDefinition{{
			Name:        "call_action",
			Label:       "Call action",
			Description: "Call a test action.",
			Risk:        connectors.RiskRead,
		}},
		prepared: &prepared,
	}
	if err := registry.Register(connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	service := NewService(registry, &fakeResolver{
		target: connectors.TargetView{
			ID:            1,
			Ref:           "api:1:2",
			ConnectorKind: "api",
			Name:          "Test API",
		},
		profile: connectors.CredentialProfileView{
			ID:            2,
			TargetID:      1,
			ConnectorKind: "api",
			Kind:          "api_key",
			Label:         "default",
		},
	})

	if _, err := service.Prepare(context.Background(), PrepareRequest{
		TargetRef:  "api:1:2",
		ActionName: "call_action",
	}); err != nil {
		t.Fatalf("prepare should allow non-secret credential references: %v", err)
	}
}

func TestLooksLikeSecretField(t *testing.T) {
	tests := map[string]bool{
		"api_key":             true,
		"user_api_key":        true,
		"authorization":       true,
		"bearer":              true,
		"credential":          true,
		"credential_hash":     true,
		"credential_id":       false,
		"credential_kind":     false,
		"credential_ref":      false,
		"credential_value":    true,
		"password":            true,
		"private_key_pem":     true,
		"profile_id":          false,
		"refresh_token":       true,
		"tenant_secret_value": true,
	}

	for field, want := range tests {
		t.Run(field, func(t *testing.T) {
			if got := looksLikeSecretField(field); got != want {
				t.Fatalf("looksLikeSecretField(%q) = %v, want %v", field, got, want)
			}
		})
	}
}
