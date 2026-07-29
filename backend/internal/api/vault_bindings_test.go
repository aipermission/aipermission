package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

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
	list := performJSON(handler, http.MethodGet, "/api/vault-default-bindings?vault_item_id="+strconv.FormatInt(item.ID, 10), "", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), target.Name) {
		t.Fatalf("list bindings = %d %s", list.Code, list.Body.String())
	}
	remove := performJSON(handler, http.MethodPost, "/api/vault-default-bindings/"+strconv.FormatInt(binding.ID, 10)+"/delete", "", deleteVaultDefaultBindingRequest{
		ExpectedBindingRevision: binding.BindingRevision,
	})
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete binding = %d %s", remove.Code, remove.Body.String())
	}
}
