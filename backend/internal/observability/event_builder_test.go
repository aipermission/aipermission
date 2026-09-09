package observability

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	gatewaydb "github.com/aipermission/aipermission/backend/internal/db"
)

func TestBuildEventExtractsConnectorMetadataAndRedactsPayload(t *testing.T) {
	tokenID := int64(9)
	event, err := BuildEvent(context.Background(), nil, BuildInput{
		ActorType: "mcp",
		TokenID:   &tokenID,
		RuntimeID: 44,
		Action:    "connector_action.completed",
		Payload: map[string]any{
			"target_ref": "postgres:12:34",
			"request_id": "56",
			"project_id": float64(78),
			"secret":     "hide-me",
		},
		Redact: func(value string) string { return strings.ReplaceAll(value, "hide-me", "[REDACTED]") },
	})
	if err != nil {
		t.Fatalf("build event: %v", err)
	}
	if event.ActorType != "mcp" || event.TokenID == nil || *event.TokenID != tokenID || event.RuntimeID != 44 {
		t.Fatalf("actor metadata = %#v", event)
	}
	if event.ConnectorKind != "postgres" || event.TargetID != 12 || event.ProfileID != 34 || event.ActionRequestID != 56 || event.ProjectID != 78 {
		t.Fatalf("connector metadata = %#v", event)
	}
	if event.LifecyclePhase != "completed" || strings.Contains(event.PayloadJSON, "hide-me") || !strings.Contains(event.PayloadJSON, "[REDACTED]") {
		t.Fatalf("event payload/lifecycle = %#v", event)
	}
}

func TestBuildEventPrefersExplicitMetadataAndResolvesProjectFallbacks(t *testing.T) {
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "audit-builder.db"), "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	projectID := queryInt64(t, database, `SELECT id FROM projects WHERE slug = 'ungrouped'`)
	targetID := insertQueryFixture(t, database, `
		INSERT INTO connector_targets (connector_kind, name, project_id, created_at, updated_at)
		VALUES ('redis', 'cache', ?, datetime('now'), datetime('now'))`, projectID)
	profileID := insertQueryFixture(t, database, `
		INSERT INTO connector_credential_profiles (target_id, connector_kind, kind, label, created_at, updated_at)
		VALUES (?, 'redis', 'password', 'default', datetime('now'), datetime('now'))`, targetID)
	runtimeID := insertQueryFixture(t, database, `
		INSERT INTO connector_runtime_surfaces (connector_kind, target_id, profile_id, capability_kind, created_at, updated_at)
		VALUES ('redis', ?, ?, 'structured_activity', datetime('now'), datetime('now'))`, targetID, profileID)

	fromTarget, err := BuildEvent(context.Background(), database, BuildInput{
		Action: "connector.checked",
		Payload: map[string]any{
			"connector_kind": "redis",
			"target_id":      targetID,
			"profile_id":     profileID,
			"target_ref":     "ssh:999:998",
		},
	})
	if err != nil {
		t.Fatalf("build target event: %v", err)
	}
	if fromTarget.ConnectorKind != "redis" || fromTarget.TargetID != targetID || fromTarget.ProfileID != profileID || fromTarget.ProjectID != projectID {
		t.Fatalf("target fallback event = %#v", fromTarget)
	}

	fromRuntime, err := BuildEvent(context.Background(), database, BuildInput{
		RuntimeID: runtimeID,
		Action:    "console.observed",
		Payload:   map[string]any{},
	})
	if err != nil {
		t.Fatalf("build runtime event: %v", err)
	}
	if fromRuntime.ProjectID != projectID || fromRuntime.LifecyclePhase != "observed" {
		t.Fatalf("runtime fallback event = %#v", fromRuntime)
	}
}

func TestBuildEventRejectsPayloadsThatCannotBeMarshaled(t *testing.T) {
	if _, err := BuildEvent(context.Background(), nil, BuildInput{Payload: make(chan int)}); err == nil {
		t.Fatal("expected payload marshal failure")
	}
}
