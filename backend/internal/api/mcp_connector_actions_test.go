package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
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
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    "no_longer_supported",
		ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set unsupported action permission: %v", err)
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
		t.Fatalf("blocked and unsupported actions should be hidden: %#v", items[0].Actions)
	}
	if len(items[0].Hints) == 0 {
		t.Fatalf("expected connector hints")
	}

	actionsResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-actions?target_ref="+connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID), token.TokenValue, nil)
	if actionsResponse.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", actionsResponse.Code, actionsResponse.Body.String())
	}
	if !strings.Contains(actionsResponse.Body.String(), postgresconnector.ActionGetSchemas) ||
		strings.Contains(actionsResponse.Body.String(), postgresconnector.ActionQueryReadonly) ||
		strings.Contains(actionsResponse.Body.String(), "no_longer_supported") {
		t.Fatalf("unexpected action discovery response: %s", actionsResponse.Body.String())
	}
}

func TestMCPProjectScopeHidesTargetsAndBlocksActions(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "project-scoped-codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	projects := projectstore.NewStore(fixture.db)
	project, err := projects.Create(ctx, "Project Alpha")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	target, err = store.UpdateTarget(ctx, connectortargets.UpdateTargetInput{
		ID:        target.ID,
		ProjectID: project.ID,
		Name:      target.Name,
		Config:    target.Config,
	})
	if err != nil {
		t.Fatalf("move target to project: %v", err)
	}
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionGetSchemas,
		ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set action permission: %v", err)
	}

	visible := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil)
	if visible.Code != http.StatusOK || !strings.Contains(visible.Body.String(), `"project_name":"Project Alpha"`) {
		t.Fatalf("project target should be visible: %d %s", visible.Code, visible.Body.String())
	}

	ungrouped, err := projects.Ungrouped(ctx)
	if err != nil {
		t.Fatalf("get ungrouped project: %v", err)
	}
	scopeResponse := performJSON(fixture.server.Handler(), http.MethodPut, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/project-scopes", "", updateTokenProjectScopesRequest{EnabledProjectIDs: []int64{ungrouped.ID}})
	if scopeResponse.Code != http.StatusOK {
		t.Fatalf("disable project scope: %d %s", scopeResponse.Code, scopeResponse.Body.String())
	}

	hidden := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil)
	if hidden.Code != http.StatusOK || hidden.Body.String() != "[]\n" {
		t.Fatalf("disabled project should be hidden: %d %s", hidden.Code, hidden.Body.String())
	}
	action := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, mcpConnectorActionCallRequest{
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionGetSchemas,
		Reason:     "verify disabled project scope",
	})
	if action.Code != http.StatusOK || !strings.Contains(action.Body.String(), `"status":"blocked"`) {
		t.Fatalf("disabled project action should be blocked: %d %s", action.Code, action.Body.String())
	}
}
