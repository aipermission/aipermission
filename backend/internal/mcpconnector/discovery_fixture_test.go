package mcpconnector_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

const discoveryPrivateCanary = "private-discovery-diagnostic-canary"

type discoveryFixture struct {
	database  *sql.DB
	store     *connectortargets.Store
	target    connectortargets.Target
	profile   connectortargets.CredentialProfile
	connector *discoveryConnector
	scope     mcpconnector.Scope
}

func newDiscoveryFixture(t *testing.T) *discoveryFixture {
	t.Helper()
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "discovery.db"), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	token, err := tokens.NewStore(database).Create(t.Context(), tokens.CreateRequest{Name: "Discovery fixture"})
	if err != nil {
		t.Fatal(err)
	}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "fixture", Name: "Target \u03bb", Config: map[string]any{"endpoint": "public-endpoint"},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: target.ConnectorKind, Kind: "test", Label: "Profile \u00e9",
		Public: map[string]any{"account": "public-account"}, EncryptedSecretJSON: discoveryPrivateCanary,
	})
	if err != nil {
		t.Fatal(err)
	}
	connector := &discoveryConnector{t: t}
	registry := connectors.NewRegistry()
	if err := registry.Register(connector); err != nil {
		t.Fatal(err)
	}
	fixture := &discoveryFixture{database: database, store: store, target: target, profile: profile, connector: connector}
	fixture.scope = mcpconnector.Scope{
		Database: database, Registry: registry.Snapshot(), TokenID: token.ID,
		MetadataEnabled: func(context.Context) (bool, error) { return true, nil },
		Metadata: func(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
			if target.ConnectorKind != "fixture" {
				t.Errorf("unexpected metadata kind: %q", target.ConnectorKind)
			}
			return connector.LiveConsoleTargetMetadata(target, profile)
		},
	}
	fixture.scope.Permissions = func(ctx context.Context) ([]mcpconnector.Permission, error) {
		permissions, err := accesscontrol.ProjectScopedSupportedConnectorPermissions(ctx, database, fixture.scope.Registry, fixture.scope.TokenID)
		if err != nil {
			return nil, err
		}
		items := make([]mcpconnector.Permission, 0, len(permissions))
		for _, permission := range permissions {
			items = append(items, mcpconnector.Permission{
				ProjectID: permission.ProjectID, ProjectName: permission.ProjectName, ProjectSlug: permission.ProjectSlug,
				TargetID: permission.TargetID, TargetName: permission.TargetName, ProfileID: permission.ProfileID,
				ProfileLabel: permission.ProfileLabel, ConnectorKind: permission.ConnectorKind, ProfileKind: permission.ProfileKind,
				ActionName: permission.ActionName, ExecutionRule: permission.ExecutionRule, ExpiresAt: permission.ExpiresAt,
			})
		}
		return items, nil
	}
	fixture.grant(t, "inspect", connectortargets.ActionPermissionAlwaysRun, nil)
	return fixture
}

func (fixture *discoveryFixture) grant(t *testing.T, action string, rule connectortargets.ActionPermissionRule, expires *time.Time) {
	t.Helper()
	if err := fixture.store.SetActionPermission(t.Context(), connectortargets.SetActionPermissionInput{
		TokenID: fixture.scope.TokenID, TargetID: fixture.target.ID, ProfileID: fixture.profile.ID,
		ActionName: action, ExecutionRule: rule, ExpiresAt: expires,
	}); err != nil {
		t.Fatal(err)
	}
}

func (fixture *discoveryFixture) request(method, ref string) *httptest.ResponseRecorder {
	handlers := mcpconnector.NewHTTPHandlers(func(http.ResponseWriter, *http.Request) (mcpconnector.Scope, bool) { return fixture.scope, true })
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?target_ref="+ref, nil)
	switch method {
	case "targets":
		handlers.ListTargets(response, request)
	case "help":
		handlers.GetHelp(response, request)
	case "actions":
		handlers.GetActions(response, request)
	default:
		panic("unknown discovery method")
	}
	return response
}

func (fixture *discoveryFixture) ref() string {
	return connectors.FormatTargetRef(fixture.target.ConnectorKind, fixture.target.ID, fixture.profile.ID)
}

func (fixture *discoveryFixture) targetView() connectors.TargetView {
	return connectors.TargetView{
		ID: fixture.target.ID, ProjectID: fixture.target.ProjectID, Ref: fixture.ref(),
		ConnectorKind: fixture.target.ConnectorKind, Name: fixture.target.Name,
		Config: fixture.target.Config, UpdatedAt: fixture.target.UpdatedAt,
	}
}

type discoveryConnector struct {
	t               *testing.T
	helpError       error
	catalogError    error
	catalogCalls    int
	metadataCalls   int
	helpTarget      connectors.TargetView
	actionTarget    connectors.TargetView
	actionProfile   connectors.CredentialProfileView
	metadataTarget  connectors.TargetView
	metadataProfile connectors.CredentialProfileView
}

func (*discoveryConnector) Kind() string                                     { return "fixture" }
func (*discoveryConnector) Label() string                                    { return "Discovery fixture" }
func (*discoveryConnector) Version() string                                  { return "1" }
func (*discoveryConnector) TargetSchema() connectors.Schema                  { return connectors.Schema{} }
func (*discoveryConnector) CredentialSchemas() []connectors.CredentialSchema { return nil }
func (connector *discoveryConnector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	connector.helpTarget = target
	return connectors.ConnectorHelp{Title: "Fixture help", Summary: "Public discovery", Connector: "fixture"}, connector.helpError
}
func (connector *discoveryConnector) GetActionList(_ context.Context, target connectors.TargetView, profile connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	connector.catalogCalls++
	connector.actionTarget, connector.actionProfile = target, profile
	return []connectors.ActionDefinition{
		{Name: "inspect", Label: "Inspect", Description: "Read fixture", Risk: connectors.RiskRead},
		{Name: "mutate", Label: "Mutate", Description: "Change fixture", Risk: connectors.RiskWrite},
	}, connector.catalogError
}
func (connector *discoveryConnector) LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
	connector.metadataCalls++
	connector.metadataTarget, connector.metadataProfile = target, profile
	return map[string]any{"endpoint": target.Config["endpoint"], "account": profile.Public["account"]}
}
func (connector *discoveryConnector) PrepareAction(context.Context, connectors.ActionRequest) (connectors.PreparedAction, error) {
	connector.t.Error("discovery prepared an execution")
	return connectors.PreparedAction{}, errors.New("discovery must not prepare")
}
func (connector *discoveryConnector) ExecuteAction(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error) {
	connector.t.Error("discovery dispatched an execution")
	return connectors.ActionResult{}, errors.New("discovery must not execute")
}
