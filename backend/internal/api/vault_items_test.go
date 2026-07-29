package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

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
	preview := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/generate-preview", "", generateVaultItemPreviewRequest{
		GeneratorKind: "hex_secret",
	})
	if preview.Code != http.StatusOK || preview.Header().Get("Cache-Control") != "no-store, private" {
		t.Fatalf("generate replacement preview: %d %s", preview.Code, preview.Body.String())
	}
	previewData := decodeRouteResponse[map[string]any](t, preview.Body.Bytes())
	previewValue, _ := previewData["value"].(string)
	previewToken, _ := previewData["preview_token"].(string)
	if len(previewValue) != 64 || previewToken == "" || strings.Contains(previewToken, previewValue) {
		t.Fatalf("unexpected generated preview response")
	}
	ambiguous := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/value", "", replaceVaultItemValueRequest{
		Source: "generated", Value: "must-not-be-ignored", GeneratorKind: "hex_secret",
		PreviewToken: previewToken, ExpectedValueVersion: replaced.ValueVersion,
	})
	if ambiguous.Code != http.StatusBadRequest {
		t.Fatalf("ambiguous replacement = %d %s", ambiguous.Code, ambiguous.Body.String())
	}
	tampered := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/value", "", replaceVaultItemValueRequest{
		Source: "generated", GeneratorKind: "hex_secret", PreviewToken: previewToken + "x",
		ExpectedValueVersion: replaced.ValueVersion,
	})
	if tampered.Code != http.StatusBadRequest {
		t.Fatalf("tampered preview = %d %s", tampered.Code, tampered.Body.String())
	}
	regenerated := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/generate-preview", "", generateVaultItemPreviewRequest{
		GeneratorKind: "hex_secret",
	})
	if regenerated.Code != http.StatusOK {
		t.Fatalf("regenerate replacement preview: %d %s", regenerated.Code, regenerated.Body.String())
	}
	regeneratedData := decodeRouteResponse[map[string]any](t, regenerated.Body.Bytes())
	regeneratedValue, _ := regeneratedData["value"].(string)
	regeneratedToken, _ := regeneratedData["preview_token"].(string)
	superseded := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/value", "", replaceVaultItemValueRequest{
		Source: "generated", GeneratorKind: "hex_secret", PreviewToken: previewToken,
		ExpectedValueVersion: replaced.ValueVersion,
	})
	if superseded.Code != http.StatusBadRequest {
		t.Fatalf("superseded preview = %d %s", superseded.Code, superseded.Body.String())
	}
	previewValue = regeneratedValue
	previewToken = regeneratedToken
	generated := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/value", "", replaceVaultItemValueRequest{
		Source: "generated", GeneratorKind: "hex_secret", PreviewToken: previewToken,
		ExpectedValueVersion: replaced.ValueVersion,
	})
	if generated.Code != http.StatusOK || strings.Contains(generated.Body.String(), "encrypted_value") {
		t.Fatalf("generate replacement vault value: %d %s", generated.Code, generated.Body.String())
	}
	generatedItem := decodeRouteResponse[projectvault.Item](t, generated.Body.Bytes())
	if generatedItem.Source != "generated" || generatedItem.GeneratorKind != "hex_secret" ||
		generatedItem.ValueVersion != replaced.ValueVersion+1 || generatedItem.GeneratorParameters["encoding"] != "hex" {
		t.Fatalf("unexpected generated replacement metadata: %#v", generatedItem)
	}
	generatedReveal := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/reveal", "", map[string]any{})
	generatedValue := decodeRouteResponse[map[string]string](t, generatedReveal.Body.Bytes())["value"]
	if generatedReveal.Code != http.StatusOK || generatedValue != previewValue || strings.Contains(generated.Body.String(), generatedValue) {
		t.Fatalf("generated replacement was invalid or leaked: %d %s", generatedReveal.Code, generated.Body.String())
	}
	importedAgain := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/value", "", replaceVaultItemValueRequest{
		Source: "imported", Value: "final-imported-secret-value", ExpectedValueVersion: generatedItem.ValueVersion,
	})
	importedItem := decodeRouteResponse[projectvault.Item](t, importedAgain.Body.Bytes())
	if importedAgain.Code != http.StatusOK || importedItem.Source != "imported" || importedItem.GeneratorKind != "" ||
		len(importedItem.GeneratorParameters) != 0 {
		t.Fatalf("imported replacement retained generator metadata: %d %#v", importedAgain.Code, importedItem)
	}
	auditAfterReplacement := performJSON(handler, http.MethodGet, "/api/audit-logs?project_id="+strconv.FormatInt(project.ID, 10), "", nil)
	if auditAfterReplacement.Code != http.StatusOK ||
		!strings.Contains(auditAfterReplacement.Body.String(), "vault.item.value_preview.generated") ||
		strings.Contains(auditAfterReplacement.Body.String(), previewValue) ||
		strings.Contains(auditAfterReplacement.Body.String(), previewToken) {
		t.Fatalf("generated preview audit was missing or leaked secret material: %d %s", auditAfterReplacement.Code, auditAfterReplacement.Body.String())
	}
	stale := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/value", "", replaceVaultItemValueRequest{
		Value: "stale-secret-value-that-stays-private", ExpectedValueVersion: item.ValueVersion,
	})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale replace = %d %s", stale.Code, stale.Body.String())
	}

	remove := performJSON(handler, http.MethodPost, "/api/vault-items/"+strconv.FormatInt(item.ID, 10)+"/delete", "", deleteVaultItemRequest{
		ExpectedValueVersion: importedItem.ValueVersion, ExpectedMetadataRevision: importedItem.MetadataRevision,
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

func TestInvalidVaultMutationDoesNotCloseActiveSession(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	projectResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/projects", "", projectRequest{Name: "Mutation Safety"})
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", projectResponse.Code, projectResponse.Body.String())
	}
	project := decodeRouteResponse[projectstore.Project](t, projectResponse.Body.Bytes())
	create := performJSON(fixture.server.Handler(), http.MethodPost, "/api/vault-items", "", createVaultItemRequest{
		Name: "MUTATION_SAFE_TOKEN", Value: "mutation-safe-secret-value",
		OwnerProjectID: project.ID, SecretType: "api_key", Source: "imported",
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create Vault item: %d %s", create.Code, create.Body.String())
	}
	item := decodeRouteResponse[projectvault.Item](t, create.Body.Bytes())
	target := fixture.createKeyAndServer(t, "mutation-session")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := fixture.db.ExecContext(ctx, `
		INSERT INTO console_sessions (runtime_id, generation, name, status, created_at, updated_at)
		VALUES (?, 1, 'mutation session', 'connected', ?, ?)`,
		target.ID, now, now,
	)
	if err != nil {
		t.Fatalf("insert active console session: %v", err)
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read active console session id: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `
		INSERT INTO vault_session_items (
			session_id, vault_item_id, vault_item_name, source_project_id, value_version,
			metadata_revision, binding_revision, created_at
		) VALUES (?, ?, ?, ?, ?, ?, 0, ?)`,
		sessionID, item.ID, item.Name, project.ID, item.ValueVersion, item.MetadataRevision, now,
	); err != nil {
		t.Fatalf("track active Vault session item: %v", err)
	}

	invalid := performJSON(
		fixture.server.Handler(),
		http.MethodPut,
		"/api/vault-items/"+strconv.FormatInt(item.ID, 10),
		"",
		updateVaultItemRequest{
			ExpectedMetadataRevision: item.MetadataRevision,
			Name:                     "invalid-name",
			OwnerProjectID:           project.ID,
			SecretType:               item.SecretType,
			ExpiryWarningDays:        item.ExpiryWarningDays,
		},
	)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid Vault mutation = %d %s", invalid.Code, invalid.Body.String())
	}
	var status string
	if err := fixture.db.QueryRowContext(ctx, `SELECT status FROM console_sessions WHERE id = ?`, sessionID).Scan(&status); err != nil {
		t.Fatalf("read active session status: %v", err)
	}
	if status != "connected" {
		t.Fatalf("failed Vault mutation closed active session: %q", status)
	}
}
