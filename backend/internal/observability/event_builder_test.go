package observability

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	gatewaydb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/projects"
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

func TestBuildEventKeepsActionProjectAfterTargetMove(t *testing.T) {
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "audit-project.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	projectA := queryInt64(t, database, `SELECT id FROM projects WHERE slug = 'ungrouped'`)
	projectB, err := projects.NewStore(database).Create(t.Context(), "Moved target")
	if err != nil {
		t.Fatal(err)
	}
	targetID := insertQueryFixture(t, database, `
		INSERT INTO connector_targets (connector_kind, name, project_id, created_at, updated_at)
		VALUES ('redis', 'cache', ?, datetime('now'), datetime('now'))`, projectA)
	profileID := insertQueryFixture(t, database, `
		INSERT INTO connector_credential_profiles (target_id, connector_kind, kind, label, created_at, updated_at)
		VALUES (?, 'redis', 'password', 'default', datetime('now'), datetime('now'))`, targetID)
	request, err := connectortargets.NewStore(database).InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
		TargetID: targetID, ProfileID: profileID, ConnectorKind: "redis", ActionName: "scan_keys", Status: connectors.ResultRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE connector_targets SET project_id = ? WHERE id = ?`, projectB.ID, targetID); err != nil {
		t.Fatal(err)
	}
	var historyProject int64
	if err := database.QueryRow(`SELECT project_id FROM history_entries WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, request.ID).Scan(&historyProject); err != nil {
		t.Fatal(err)
	}
	event, err := BuildEvent(t.Context(), database, BuildInput{Action: "connector_action.completed", Payload: map[string]any{
		"request_id": request.ID, "target_id": targetID, "profile_id": profileID, "connector_kind": "redis",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if historyProject != projectA || event.ProjectID != projectA {
		t.Fatalf("moved action project: history=%d audit=%d want=%d", historyProject, event.ProjectID, projectA)
	}
	newEvent, err := BuildEvent(t.Context(), database, BuildInput{Action: "connector.checked", Payload: map[string]any{"target_id": targetID}})
	if err != nil || newEvent.ProjectID != projectB.ID {
		t.Fatalf("new target event project = %d, want %d: %v", newEvent.ProjectID, projectB.ID, err)
	}
	unrelatedEvent, err := BuildEvent(t.Context(), database, BuildInput{Action: "vault.item.observed", Payload: map[string]any{
		"request_id": request.ID, "target_id": targetID,
	}})
	if err != nil || unrelatedEvent.ProjectID != projectB.ID {
		t.Fatalf("unrelated request inherited connector project: project=%d err=%v", unrelatedEvent.ProjectID, err)
	}
}
