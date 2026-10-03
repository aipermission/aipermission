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
	for _, value := range []any{true, 42, map[string]any{"allowed": "example.test"}, []any{"example.test", nil}} {
		config["allowed_recipient_domains"] = value
		for _, operation := range []struct {
			method, path string
			body         any
		}{
			{http.MethodPost, "/api/connector-targets", createConnectorTargetRequest{ConnectorKind: "mail", Name: "invalid-mail", Config: config}},
			{http.MethodPut, path, updateConnectorTargetRequest{Name: "invalid-mail", Config: config}},
		} {
			response := performJSON(handler, operation.method, operation.path, "", operation.body)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "allowed_recipient_domains") {
				t.Fatalf("malformed policy admitted by %s: %d %s", operation.method, response.Code, response.Body.String())
			}
		}
	}
	var count int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM connector_targets WHERE name = 'invalid-mail'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid policy persisted: count=%d err=%v", count, err)
	}
	readback := performJSON(handler, http.MethodGet, path, "", nil)
	if readback.Code != http.StatusOK || !strings.Contains(readback.Body.String(), `"allowed_recipient_domains":["example.test"]`) {
		t.Fatalf("rejected update changed existing policy: %d %s", readback.Code, readback.Body.String())
	}
}
