package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestConnectorCredentialBoundaryAcrossRESTMCPHistoryAndAudit(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	runtime := fixture.server.activeRuntime()
	if err := runtime.registry.Register(localActionTestConnector{}); err != nil {
		t.Fatalf("register local test connector: %v", err)
	}

	const credentialSecret = "gateway-credential-never-return-7f3a"
	const targetOutput = "permitted-target-output-may-be-sensitive-7f3a"
	store := connectortargets.NewStore(fixture.db)
	target, err := store.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: localActionTestConnectorKind,
		Name:          "credential-boundary-target",
		Config:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("create connector target: %v", err)
	}
	profile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       localActionTestConnectorKind,
		Kind:                "default",
		Label:               "main",
		Public:              map[string]any{"username": "visible-user"},
		EncryptedSecretJSON: "",
	})
	if err != nil {
		t.Fatalf("create connector credential profile: %v", err)
	}
	encryptedSecret, err := recordcrypto.EncryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profile.ID, map[string]any{"password": credentialSecret})
	if err != nil {
		t.Fatalf("encrypt connector credential: %v", err)
	}
	if err := store.SetCredentialProfileEncryptedSecret(ctx, target.ID, profile.ID, encryptedSecret); err != nil {
		t.Fatalf("store connector credential: %v", err)
	}
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "credential-boundary-token"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    "echo",
		ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set connector action permission: %v", err)
	}
	if err := writeSecuritySettings(ctx, runtime, securitySettingsResponse{RedactionMode: redactionModeOff}); err != nil {
		t.Fatalf("disable operator-configured redaction: %v", err)
	}

	assertCredentialAbsent := func(label string, body string) {
		t.Helper()
		if strings.Contains(body, credentialSecret) {
			t.Fatalf("%s exposed the gateway-held connector credential: %s", label, body)
		}
	}
	assertOKWithoutCredential := func(label string, responseBody string, status int) {
		t.Helper()
		if status != http.StatusOK {
			t.Fatalf("%s returned %d: %s", label, status, responseBody)
		}
		assertCredentialAbsent(label, responseBody)
	}

	targetResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10), "", nil)
	assertOKWithoutCredential("REST target response", targetResponse.Body.String(), targetResponse.Code)
	profilesResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/profiles", "", nil)
	assertOKWithoutCredential("REST profile response", profilesResponse.Body.String(), profilesResponse.Code)
	if !strings.Contains(profilesResponse.Body.String(), "visible-user") {
		t.Fatalf("REST profile response should retain non-secret public metadata: %s", profilesResponse.Body.String())
	}
	connectionTestResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/profiles/"+strconv.FormatInt(profile.ID, 10)+"/test", "", nil)
	assertOKWithoutCredential("connection test response", connectionTestResponse.Body.String(), connectionTestResponse.Code)

	mcpTargetsResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil)
	assertOKWithoutCredential("MCP target discovery response", mcpTargetsResponse.Body.String(), mcpTargetsResponse.Code)
	actionResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, mcpConnectorActionCallRequest{
		TargetRef:      connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName:     "echo",
		Input:          map[string]any{"value": "reflect-credential"},
		Reason:         "verify connector credential output boundary",
		IdempotencyKey: "credential-output-boundary",
	})
	assertOKWithoutCredential("MCP connector action response", actionResponse.Body.String(), actionResponse.Code)
	actionResult := decodeRouteResponse[mcpConnectorActionResponse](t, actionResponse.Body.Bytes())
	if actionResult.Status != string(connectors.ResultCompleted) || !strings.Contains(actionResult.DisplayText, targetOutput) || !strings.Contains(actionResult.DisplayText, connectorCredentialRedactionMarker) {
		t.Fatalf("permitted target output should remain visible to the caller: %#v", actionResult)
	}

	historyResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?connector_kind="+localActionTestConnectorKind+"&limit=10", "", nil)
	assertOKWithoutCredential("history list response", historyResponse.Body.String(), historyResponse.Code)
	historyPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, historyResponse.Body.Bytes())
	if len(historyPage.Items) != 1 {
		t.Fatalf("expected one connector history item, got %#v", historyPage)
	}
	historyDetailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history/"+strconv.FormatInt(historyPage.Items[0].ID, 10), "", nil)
	assertOKWithoutCredential("history detail response", historyDetailResponse.Body.String(), historyDetailResponse.Code)
	historyDetail := decodeRouteResponse[historyEntryRecord](t, historyDetailResponse.Body.Bytes())
	if !strings.Contains(historyDetail.OutputText, targetOutput) || !strings.Contains(historyDetail.OutputJSON, targetOutput) {
		t.Fatalf("history should preserve permitted target output: %#v", historyDetail)
	}

	auditResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/audit-logs?connector_kind="+localActionTestConnectorKind+"&target_id="+strconv.FormatInt(target.ID, 10), "", nil)
	assertOKWithoutCredential("audit list response", auditResponse.Body.String(), auditResponse.Code)
	auditPage := decodeRouteResponse[pageResponse[auditLogRecord]](t, auditResponse.Body.Bytes())
	if len(auditPage.Items) < 3 {
		t.Fatalf("expected connector request lifecycle audit items, got %#v", auditPage)
	}
	for _, auditItem := range auditPage.Items {
		auditDetailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/audit-logs/"+strconv.FormatInt(auditItem.ID, 10), "", nil)
		assertOKWithoutCredential("audit detail response", auditDetailResponse.Body.String(), auditDetailResponse.Code)
	}

	var persistedSecretReferences int
	if err := fixture.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM connector_action_requests
			 WHERE input_json LIKE ? OR output_json LIKE ? OR display_text LIKE ? OR error LIKE ?)
			+
			(SELECT COUNT(*) FROM history_entries
			 WHERE preview_json LIKE ? OR input_text LIKE ? OR input_json LIKE ?
			    OR output_text LIKE ? OR output_json LIKE ? OR error LIKE ?)
			+
			(SELECT COUNT(*) FROM audit_logs WHERE payload_json LIKE ?)`,
		"%"+credentialSecret+"%", "%"+credentialSecret+"%", "%"+credentialSecret+"%", "%"+credentialSecret+"%",
		"%"+credentialSecret+"%", "%"+credentialSecret+"%", "%"+credentialSecret+"%",
		"%"+credentialSecret+"%", "%"+credentialSecret+"%", "%"+credentialSecret+"%",
		"%"+credentialSecret+"%",
	).Scan(&persistedSecretReferences); err != nil {
		t.Fatalf("inspect persisted connector output surfaces: %v", err)
	}
	if persistedSecretReferences != 0 {
		t.Fatalf("gateway-held connector credential appeared in %d persisted output surfaces", persistedSecretReferences)
	}
}

func TestConnectorCredentialBoundaryRedactsEncodedVariants(t *testing.T) {
	boundary := newConnectorCredentialBoundary(map[string]any{
		"password": "credential+/value",
		"nested":   map[string]any{"token": "second-value"},
	})
	for _, value := range []string{
		"credential+/value",
		"credential%2B%2Fvalue",
		"credential%2B%2Fvalue",
		"Y3JlZGVudGlhbCsvdmFsdWU=",
		"second-value",
	} {
		if redacted := boundary.Redact("prefix " + value + " suffix"); strings.Contains(redacted, value) || !strings.Contains(redacted, connectorCredentialRedactionMarker) {
			t.Fatalf("credential variant was not redacted: input=%q output=%q", value, redacted)
		}
	}
}
