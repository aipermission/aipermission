package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

func TestVaultItemLocalRoutesKeepValuesOutOfMetadataAndAudit(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	projectResponse := performJSON(handler, http.MethodPost, "/api/projects", "", projectRequest{Name: "Vault Project"})
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", projectResponse.Code, projectResponse.Body.String())
	}
	project := decodeRouteResponse[projectstore.Project](t, projectResponse.Body.Bytes())
	secretValue := "test-secret-value-that-must-not-leak"

	create := performJSON(handler, http.MethodPost, "/api/vault-items", "", createVaultItemRequest{
		Name: "VAULT_TEST_API_KEY", Value: secretValue, OwnerProjectID: project.ID,
		SecretType: "api_key", Source: "imported", ExpiryWarningDays: 14,
		Provider: "Test API", Tags: []string{"test"},
		UsageNotes: []vaultUsageNoteRequest{{Location: "local test"}},
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create vault item: %d %s", create.Code, create.Body.String())
	}
	if strings.Contains(create.Body.String(), secretValue) || strings.Contains(create.Body.String(), "encrypted_value") {
		t.Fatalf("create response leaked secret material: %s", create.Body.String())
	}
	item := decodeRouteResponse[projectvault.Item](t, create.Body.Bytes())

	list := performJSON(handler, http.MethodGet, "/api/vault-items?project_id="+strconv.FormatInt(project.ID, 10), "", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), item.Name) || strings.Contains(list.Body.String(), secretValue) {
		t.Fatalf("unexpected vault list: %d %s", list.Code, list.Body.String())
	}

	reveal := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/reveal", "", map[string]any{})
	if reveal.Code != http.StatusOK || !strings.Contains(reveal.Body.String(), secretValue) {
		t.Fatalf("reveal vault item: %d %s", reveal.Code, reveal.Body.String())
	}
	if reveal.Header().Get("Cache-Control") != "no-store, private" || reveal.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("reveal cache headers = %#v", reveal.Header())
	}

	audit := performJSON(handler, http.MethodGet, "/api/audit-logs?project_id="+strconv.FormatInt(project.ID, 10), "", nil)
	if audit.Code != http.StatusOK || !strings.Contains(audit.Body.String(), "vault.item.revealed") {
		t.Fatalf("vault audit missing: %d %s", audit.Code, audit.Body.String())
	}
	if strings.Contains(audit.Body.String(), secretValue) {
		t.Fatalf("audit leaked vault secret: %s", audit.Body.String())
	}

	replace := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/value", "", replaceVaultItemValueRequest{
		Value: "replacement-secret-value-that-stays-private", ExpectedValueVersion: item.ValueVersion,
	})
	if replace.Code != http.StatusOK || strings.Contains(replace.Body.String(), "replacement-secret") {
		t.Fatalf("replace vault item: %d %s", replace.Code, replace.Body.String())
	}
	replaced := decodeRouteResponse[projectvault.Item](t, replace.Body.Bytes())
	stale := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/value", "", replaceVaultItemValueRequest{
		Value: "stale-secret-value-that-stays-private", ExpectedValueVersion: item.ValueVersion,
	})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale replace = %d %s", stale.Code, stale.Body.String())
	}

	remove := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/delete", "", deleteVaultItemRequest{
		ExpectedValueVersion: replaced.ValueVersion, ExpectedMetadataRevision: replaced.MetadataRevision,
	})
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete vault item: %d %s", remove.Code, remove.Body.String())
	}
}

func TestVaultRevealRateLimitIsBounded(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	projectResponse := performJSON(handler, http.MethodPost, "/api/projects", "", projectRequest{Name: "Rate Limit"})
	project := decodeRouteResponse[projectstore.Project](t, projectResponse.Body.Bytes())
	create := performJSON(handler, http.MethodPost, "/api/vault-items", "", createVaultItemRequest{
		Name: "RATE_LIMIT_SECRET", Value: "rate-limit-secret-value", OwnerProjectID: project.ID,
		SecretType: "generic_secret", Source: "imported", ExpiryWarningDays: 14,
	})
	item := decodeRouteResponse[projectvault.Item](t, create.Body.Bytes())
	path := "/api/vault-items/" + strconv.FormatInt(item.ID, 10) + "/reveal"
	for index := 0; index < 8; index++ {
		if response := performJSON(handler, http.MethodPost, path, "", map[string]any{}); response.Code != http.StatusOK {
			t.Fatalf("reveal %d = %d %s", index, response.Code, response.Body.String())
		}
	}
	if response := performJSON(handler, http.MethodPost, path, "", map[string]any{}); response.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited reveal = %d %s", response.Code, response.Body.String())
	}
}
