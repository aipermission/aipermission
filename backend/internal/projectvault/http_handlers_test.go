package projectvault

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func TestHTTPHandlersCreateAndRevealSensitiveItem(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Runtime: harness.runtime, RuntimeID: "workspace-one"}, true
	})

	create := vaultHTTPRequest(t, http.MethodPost, "/api/vault-items", `{
		"name":"HTTP_SECRET","value":"http-secret-value","owner_project_id":`+
		strconv.FormatInt(harness.projectID, 10)+`,"secret_type":"generic_secret","source":"imported"
	}`)
	created := httptest.NewRecorder()
	handlers.CreateItem(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", created.Code, created.Body.String())
	}
	var item Item
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.Name != "HTTP_SECRET" || strings.Contains(created.Body.String(), "http-secret-value") {
		t.Fatalf("created item = %s", created.Body.String())
	}

	reveal := httptest.NewRequest(http.MethodPost, "/api/vault-items/1/reveal", strings.NewReader(`{}`))
	reveal.SetPathValue("id", strconv.FormatInt(item.ID, 10))
	revealed := httptest.NewRecorder()
	handlers.RevealItem(revealed, reveal)
	if revealed.Code != http.StatusOK || revealed.Header().Get("Cache-Control") != "no-store, private" {
		t.Fatalf("reveal response = %d headers %#v body %s", revealed.Code, revealed.Header(), revealed.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(revealed.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["value"] != "http-secret-value" {
		t.Fatalf("revealed payload = %#v", payload)
	}
}

func TestHTTPHandlersPreserveFailClosedTransportContracts(t *testing.T) {
	t.Run("locked scope", func(t *testing.T) {
		handlers := NewHTTPHandlers(func(w http.ResponseWriter) (HTTPScope, bool) {
			httptransport.WriteError(w, http.StatusLocked, "database is locked")
			return HTTPScope{}, false
		})
		response := httptest.NewRecorder()
		handlers.ListItems(response, httptest.NewRequest(http.MethodGet, "/api/vault-items", nil))
		if response.Code != http.StatusLocked {
			t.Fatalf("status = %d", response.Code)
		}
	})

	t.Run("strict json", func(t *testing.T) {
		harness := newRuntimeTestHarness(t)
		handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
			return HTTPScope{Runtime: harness.runtime, RuntimeID: "workspace-one"}, true
		})
		response := httptest.NewRecorder()
		handlers.CreateItem(response, vaultHTTPRequest(t, http.MethodPost, "/api/vault-items", `{"unexpected":true}`))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
		}
	})

	t.Run("canceled mutation", func(t *testing.T) {
		harness := newRuntimeTestHarness(t)
		harness.delivery.err = context.Canceled
		handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
			return HTTPScope{Runtime: harness.runtime, RuntimeID: "workspace-one"}, true
		})
		response := httptest.NewRecorder()
		handlers.CreateItem(response, vaultHTTPRequest(t, http.MethodPost, "/api/vault-items", `{}`))
		if response.Code != http.StatusRequestTimeout {
			t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
		}
	})
}

func TestHTTPHandlersRejectMalformedFiltersAndIDs(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Runtime: harness.runtime, RuntimeID: "workspace-one"}, true
	})
	for _, path := range []string{
		"/api/vault-items?project_id=zero",
		"/api/vault-items?limit=many",
		"/api/vault-items?offset=next",
		"/api/vault-default-bindings?profile_id=-1",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if strings.Contains(path, "default-bindings") {
			handlers.ListDefaultBindings(response, request)
		} else {
			handlers.ListItems(response, request)
		}
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d body = %s", path, response.Code, response.Body.String())
		}
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/vault-items/nope", nil)
	request.SetPathValue("id", "nope")
	handlers.GetItem(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d", response.Code)
	}
}

func TestHTTPHandlersOwnVaultItemAndBindingLifecycle(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	item := harness.create(t)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Runtime: harness.runtime, RuntimeID: "workspace-one"}, true
	})

	update := vaultHTTPRequest(t, http.MethodPut, "/api/vault-items/1", `{
		"expected_metadata_revision":1,"name":"UPDATED_SECRET","owner_project_id":`+
		strconv.FormatInt(harness.projectID, 10)+`,"secret_type":"api_key","provider":"internal",
		"environment":"test","description":"updated over HTTP","expiry_warning_days":7,
		"tags":["deployment"],"usage_notes":[{"location":"service.env","notes":"runtime"}]
	}`)
	update.SetPathValue("id", strconv.FormatInt(item.ID, 10))
	updatedResponse := httptest.NewRecorder()
	handlers.UpdateItem(updatedResponse, update)
	if updatedResponse.Code != http.StatusOK || strings.Contains(updatedResponse.Body.String(), "runtime-secret-value") {
		t.Fatalf("update response = %d %s", updatedResponse.Code, updatedResponse.Body.String())
	}
	var updated Item
	if err := json.Unmarshal(updatedResponse.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "UPDATED_SECRET" || updated.MetadataRevision != item.MetadataRevision+1 {
		t.Fatalf("updated item = %#v", updated)
	}

	previewRequest := vaultHTTPRequest(t, http.MethodPost, "/api/vault-items/1/generate-preview", `{"generator_kind":"hex_secret"}`)
	previewRequest.SetPathValue("id", strconv.FormatInt(item.ID, 10))
	previewResponse := httptest.NewRecorder()
	handlers.GenerateItemPreview(previewResponse, previewRequest)
	if previewResponse.Code != http.StatusOK || previewResponse.Header().Get("Cache-Control") != "no-store, private" {
		t.Fatalf("preview response = %d headers=%#v body=%s", previewResponse.Code, previewResponse.Header(), previewResponse.Body.String())
	}
	var preview struct {
		Value        string `json:"value"`
		PreviewToken string `json:"preview_token"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Value == "" || preview.PreviewToken == "" {
		t.Fatalf("preview = %#v", preview)
	}

	replaceBody, err := json.Marshal(ReplaceValueHTTPRequest{
		Source: "generated", GeneratorKind: "hex_secret", PreviewToken: preview.PreviewToken,
		ExpectedValueVersion: updated.ValueVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	replaceRequest := vaultHTTPRequest(t, http.MethodPut, "/api/vault-items/1/value", string(replaceBody))
	replaceRequest.SetPathValue("id", strconv.FormatInt(item.ID, 10))
	replaceResponse := httptest.NewRecorder()
	handlers.ReplaceItemValue(replaceResponse, replaceRequest)
	if replaceResponse.Code != http.StatusOK || strings.Contains(replaceResponse.Body.String(), preview.Value) {
		t.Fatalf("replace response = %d %s", replaceResponse.Code, replaceResponse.Body.String())
	}
	var replaced Item
	if err := json.Unmarshal(replaceResponse.Body.Bytes(), &replaced); err != nil {
		t.Fatal(err)
	}

	targetStore := connectortargets.NewStore(harness.database)
	target, err := targetStore.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "test", Name: "http-binding-target", Config: map[string]any{"endpoint": "local"},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := targetStore.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "test", Kind: "test", Label: "default",
		Public: map[string]any{"identity": "runtime"},
	})
	if err != nil {
		t.Fatal(err)
	}
	bindingBody, err := json.Marshal(SaveDefaultBindingHTTPRequest{
		VaultItemID: item.ID, SourceProjectID: harness.projectID, TargetID: target.ID,
		ProfileID: profile.ID, ReplaceExisting: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	bindingResponse := httptest.NewRecorder()
	handlers.SaveDefaultBinding(bindingResponse, vaultHTTPRequest(t, http.MethodPut, "/api/vault-default-bindings", string(bindingBody)))
	if bindingResponse.Code != http.StatusOK {
		t.Fatalf("save binding response = %d %s", bindingResponse.Code, bindingResponse.Body.String())
	}
	var binding DefaultBinding
	if err := json.Unmarshal(bindingResponse.Body.Bytes(), &binding); err != nil {
		t.Fatal(err)
	}

	listResponse := httptest.NewRecorder()
	handlers.ListDefaultBindings(listResponse, httptest.NewRequest(http.MethodGet,
		"/api/vault-default-bindings?vault_item_id="+strconv.FormatInt(item.ID, 10)+
			"&target_id="+strconv.FormatInt(target.ID, 10)+"&profile_id="+strconv.FormatInt(profile.ID, 10), nil))
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"id":`+strconv.FormatInt(binding.ID, 10)) {
		t.Fatalf("list binding response = %d %s", listResponse.Code, listResponse.Body.String())
	}

	deleteBindingBody := `{"expected_binding_revision":` + strconv.FormatInt(binding.BindingRevision, 10) + `}`
	deleteBindingRequest := vaultHTTPRequest(t, http.MethodDelete, "/api/vault-default-bindings/1", deleteBindingBody)
	deleteBindingRequest.SetPathValue("id", strconv.FormatInt(binding.ID, 10))
	deleteBindingResponse := httptest.NewRecorder()
	handlers.DeleteDefaultBinding(deleteBindingResponse, deleteBindingRequest)
	if deleteBindingResponse.Code != http.StatusNoContent {
		t.Fatalf("delete binding response = %d %s", deleteBindingResponse.Code, deleteBindingResponse.Body.String())
	}

	deleteBody := `{"expected_value_version":` + strconv.FormatInt(replaced.ValueVersion, 10) +
		`,"expected_metadata_revision":` + strconv.FormatInt(replaced.MetadataRevision, 10) + `}`
	deleteRequest := vaultHTTPRequest(t, http.MethodDelete, "/api/vault-items/1", deleteBody)
	deleteRequest.SetPathValue("id", strconv.FormatInt(item.ID, 10))
	deleteResponse := httptest.NewRecorder()
	handlers.DeleteItem(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete item response = %d %s", deleteResponse.Code, deleteResponse.Body.String())
	}

	missingRequest := httptest.NewRequest(http.MethodGet, "/api/vault-items/1", nil)
	missingRequest.SetPathValue("id", strconv.FormatInt(item.ID, 10))
	missingResponse := httptest.NewRecorder()
	handlers.GetItem(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing item response = %d %s", missingResponse.Code, missingResponse.Body.String())
	}
}

func vaultHTTPRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}
