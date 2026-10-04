package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestMailRoutesRejectMalformedRecipientPolicyWithoutPersistence(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	config := map[string]any{"imap_host": "imap.example.test", "smtp_host": "smtp.example.test", "allowed_recipient_domains": []string{"example.test"}}
	created := performJSON(handler, http.MethodPost, "/api/connector-targets", "", createConnectorTargetRequest{ConnectorKind: "mail", Name: "valid-mail", Config: config})
	if created.Code != http.StatusCreated {
		t.Fatalf("valid policy rejected: %d %s", created.Code, created.Body.String())
	}
	target := decodeRouteResponse[connectorTargetResponse](t, created.Body.Bytes())
	path := "/api/connector-targets/" + strconv.FormatInt(target.ID, 10)
	profile := createConnectorCredentialProfileRequest{Kind: "password", Label: "mail-policy", Public: map[string]any{"mailbox_address": "reader@example.test"}, Secret: map[string]any{"imap_username": "fixture", "imap_password": "fixture-only"}}
	combined := performJSON(handler, http.MethodPost, "/api/connector-targets/with-profile", "", createConnectorTargetWithProfileRequest{
		Target: createConnectorTargetRequest{ConnectorKind: "mail", Name: "valid-combined", Config: config}, Profile: profile,
	})
	if combined.Code != http.StatusCreated || len(decodeRouteResponse[connectorTargetResponse](t, combined.Body.Bytes()).Profiles) != 1 {
		t.Fatalf("valid combined policy rejected: %d %s", combined.Code, combined.Body.String())
	}
	var count int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM connector_credential_profiles WHERE label = 'mail-policy'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("valid combined credential was not persisted: count=%d err=%v", count, err)
	}
	profile.Label = "invalid-mail"
	invalid := []any{true, 42, map[string]any{"allowed": "example.test"}, []any{"example.test", nil}}
	for _, value := range invalid {
		config["allowed_recipient_domains"] = value
		for _, operation := range []struct {
			method, path string
			body         any
		}{
			{http.MethodPost, "/api/connector-targets", createConnectorTargetRequest{ConnectorKind: "mail", Name: "invalid-mail", Config: config}},
			{http.MethodPut, path, updateConnectorTargetRequest{Name: "invalid-mail", Config: config}},
			{http.MethodPost, "/api/connector-targets/with-profile", createConnectorTargetWithProfileRequest{Target: createConnectorTargetRequest{ConnectorKind: "mail", Name: "invalid-mail", Config: config}, Profile: profile}},
		} {
			response := performJSON(handler, operation.method, operation.path, "", operation.body)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "allowed_recipient_domains") {
				t.Fatalf("malformed policy admitted by %s: %d %s", operation.method, response.Code, response.Body.String())
			}
		}
	}
	config["allowed_recipient_domains"] = []string{"example.test"}
	for _, field := range []string{"allowed_read_folders", "allowed_mutation_source_folders", "allowed_mutation_destination_folders"} {
		for _, value := range invalid {
			profile.Public[field] = value
			response := performJSON(handler, http.MethodPost, "/api/connector-targets/with-profile", "", createConnectorTargetWithProfileRequest{
				Target: createConnectorTargetRequest{ConnectorKind: "mail", Name: "invalid-mail", Config: config}, Profile: profile,
			})
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), field) {
				t.Fatalf("malformed combined folder policy admitted: field=%s status=%d body=%s", field, response.Code, response.Body.String())
			}
		}
		delete(profile.Public, field)
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM connector_targets WHERE name = 'invalid-mail'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid policy persisted: count=%d err=%v", count, err)
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM connector_credential_profiles WHERE label = 'invalid-mail'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid credential policy persisted: count=%d err=%v", count, err)
	}
	readback := performJSON(handler, http.MethodGet, path, "", nil)
	if readback.Code != http.StatusOK || !strings.Contains(readback.Body.String(), `"allowed_recipient_domains":["example.test"]`) {
		t.Fatalf("rejected update changed existing policy: %d %s", readback.Code, readback.Body.String())
	}
}
