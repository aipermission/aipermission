package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestMCPListConnectorTargetsUsesActionPermissions(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionGetSchemas,
		ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set allowed action permission: %v", err)
	}
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionBlocked,
	}); err != nil {
		t.Fatalf("set blocked action permission: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var items []mcpConnectorTargetItem
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one target/profile, got %#v", items)
	}
	if items[0].TargetRef != connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID) {
		t.Fatalf("target ref = %q", items[0].TargetRef)
	}
	if len(items[0].Actions) != 1 || items[0].Actions[0].Name != postgresconnector.ActionGetSchemas {
		t.Fatalf("blocked actions should be hidden: %#v", items[0].Actions)
	}
	if len(items[0].Hints) == 0 {
		t.Fatalf("expected connector hints")
	}
}
