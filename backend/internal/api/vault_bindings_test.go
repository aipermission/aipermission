package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

func TestVaultDefaultBindingLocalRoutes(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	projectResponse := performJSON(handler, http.MethodPost, "/api/projects", "", projectRequest{Name: "Bindings"})
	project := decodeRouteResponse[projectstore.Project](t, projectResponse.Body.Bytes())
	create := performJSON(handler, http.MethodPost, "/api/vault-items", "", createVaultItemRequest{
		Name: "ROUTE_BINDING_SECRET", Value: "route-binding-secret-value",
		OwnerProjectID: project.ID, SecretType: "generic_secret",
		Source: "imported", ExpiryWarningDays: 14,
	})
	item := decodeRouteResponse[projectvault.Item](t, create.Body.Bytes())
	target := fixture.createKeyAndServer(t, "binding-route")

	save := performJSON(handler, http.MethodPut, "/api/vault-default-bindings", "", saveVaultDefaultBindingRequest{
		VaultItemID: item.ID, SourceProjectID: project.ID,
		TargetID: target.TargetID, ProfileID: target.ProfileID,
	})
	if save.Code != http.StatusOK || strings.Contains(save.Body.String(), "route-binding-secret") {
		t.Fatalf("save binding = %d %s", save.Code, save.Body.String())
	}
	binding := decodeRouteResponse[projectvault.DefaultBinding](t, save.Body.Bytes())
	noOp := performJSON(handler, http.MethodPut, "/api/vault-default-bindings", "", saveVaultDefaultBindingRequest{
		VaultItemID: item.ID, SourceProjectID: project.ID,
		TargetID: target.TargetID, ProfileID: target.ProfileID,
		ExpectedBindingRevision: binding.BindingRevision,
	})
	if noOp.Code != http.StatusOK {
		t.Fatalf("save unchanged binding = %d %s", noOp.Code, noOp.Body.String())
	}
	unchanged := decodeRouteResponse[projectvault.DefaultBinding](t, noOp.Body.Bytes())
	if unchanged.BindingRevision != binding.BindingRevision {
		t.Fatalf("unchanged binding advanced revision: before=%d after=%d", binding.BindingRevision, unchanged.BindingRevision)
	}
	var updatedAudits int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'vault.binding.updated'`).Scan(&updatedAudits); err != nil {
		t.Fatal(err)
	}
	if updatedAudits != 1 {
		t.Fatalf("unchanged binding emitted an updated audit: %d", updatedAudits)
	}
	list := performJSON(handler, http.MethodGet, "/api/vault-default-bindings?vault_item_id="+strconv.FormatInt(item.ID, 10), "", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), target.Name) {
		t.Fatalf("list bindings = %d %s", list.Code, list.Body.String())
	}
	options := performJSON(handler, http.MethodGet, "/api/vault-session-options?runtime_id="+strconv.FormatInt(target.ID, 10), "", nil)
	if options.Code != http.StatusOK || !strings.Contains(options.Body.String(), `"supported":true`) ||
		!strings.Contains(options.Body.String(), `"target_project_id":1`) ||
		!strings.Contains(options.Body.String(), `"ROUTE_BINDING_SECRET"`) ||
		strings.Contains(options.Body.String(), "route-binding-secret-value") {
		t.Fatalf("session options = %d %s", options.Code, options.Body.String())
	}
	remove := performJSON(handler, http.MethodPost, "/api/vault-default-bindings/"+strconv.FormatInt(binding.ID, 10)+"/delete", "", deleteVaultDefaultBindingRequest{
		ExpectedBindingRevision: binding.BindingRevision,
	})
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete binding = %d %s", remove.Code, remove.Body.String())
	}
}

func TestVaultDefaultBindingRejectsProfileWithoutSessionEnvironmentCapability(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	projectResponse := performJSON(handler, http.MethodPost, "/api/projects", "", projectRequest{Name: "Unsupported Binding"})
	project := decodeRouteResponse[projectstore.Project](t, projectResponse.Body.Bytes())
	create := performJSON(handler, http.MethodPost, "/api/vault-items", "", createVaultItemRequest{
		Name: "UNSUPPORTED_BINDING_SECRET", Value: "unsupported-binding-secret-value",
		OwnerProjectID: project.ID, SecretType: "generic_secret",
		Source: "imported", ExpiryWarningDays: 14,
	})
	item := decodeRouteResponse[projectvault.Item](t, create.Body.Bytes())
	store := connectortargets.NewStore(fixture.db)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "postgres", ProjectID: project.ID, Name: "Unsupported Postgres",
		Config: map[string]any{"host": "127.0.0.1", "port": 5432, "database": "postgres"},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: target.ConnectorKind,
		Kind: "password", Label: "readonly", Public: map[string]any{"username": "readonly"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureRuntimeSurface(t.Context(), connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: target.ConnectorKind, TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: connectortargets.RuntimeCapabilityLiveConsole, Label: profile.Label,
	}); err != nil {
		t.Fatal(err)
	}

	save := performJSON(handler, http.MethodPut, "/api/vault-default-bindings", "", saveVaultDefaultBindingRequest{
		VaultItemID: item.ID, SourceProjectID: project.ID,
		TargetID: target.ID, ProfileID: profile.ID,
	})
	if save.Code != http.StatusConflict || !strings.Contains(save.Body.String(), "does not support Vault session environments") {
		t.Fatalf("unsupported binding = %d %s", save.Code, save.Body.String())
	}
}
