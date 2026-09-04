package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

const (
	secretLeakCanaryConnectorKind = "secretcanary"
	secretLeakCanaryValue         = "AIPERMISSION_SECRET_CANARY_43_9f7c2e"
	secretLeakInputCanaryValue    = "OPAQUE_INPUT_CANARY_43_7ac418"
)

type secretLeakCanaryConnector struct{}

func (secretLeakCanaryConnector) Kind() string    { return secretLeakCanaryConnectorKind }
func (secretLeakCanaryConnector) Label() string   { return "Secret Canary" }
func (secretLeakCanaryConnector) Version() string { return "0.1" }
func (secretLeakCanaryConnector) TargetSchema() connectors.Schema {
	return connectors.Schema{}
}

func (secretLeakCanaryConnector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{
		Kind:  "default",
		Label: "Default",
		Schema: connectors.Schema{Fields: []connectors.Field{{
			Name: "password", Label: "Password", Type: connectors.FieldSecret, Secret: true, Required: true,
		}}},
	}}
}

func (secretLeakCanaryConnector) GetHelp(context.Context, connectors.TargetView) (connectors.ConnectorHelp, error) {
	return connectors.ConnectorHelp{
		Title: "Secret leak canary", Summary: "Security boundary test connector.",
		Connector: "Secret Canary", ConnectorID: secretLeakCanaryConnectorKind,
	}, nil
}

func (secretLeakCanaryConnector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{{
		Name: "emit_secret", Label: "Emit secret", Description: "Attempt to emit a canary through every result surface.",
		Risk: connectors.RiskRead,
		InputSchema: connectors.Schema{Fields: []connectors.Field{{
			Name: "payload", Label: "Payload", Type: connectors.FieldString, Required: true,
		}}},
		SensitiveInputFields: []string{"payload"},
		OutputHint:           connectors.OutputHint{Format: "json", SensitiveFields: []string{"canary"}},
	}}, nil
}

func (secretLeakCanaryConnector) PrepareAction(_ context.Context, request connectors.ActionRequest) (connectors.PreparedAction, error) {
	payload, _ := request.Input["payload"].(string)
	return connectors.PreparedAction{
		ConnectorKind: secretLeakCanaryConnectorKind,
		TargetRef:     request.Target.Ref,
		ProfileID:     request.Profile.ID,
		ActionName:    request.ActionName,
		Risk:          connectors.RiskRead,
		Title:         "Emit secret canary",
		Summary:       "Exercise every redacted connector result boundary.",
		Preview:       map[string]any{"payload": "[sensitive input omitted]"},
		Payload:       map[string]any{"payload": payload},
		ContextMaterial: map[string]any{
			"purpose": "secret output boundary canary",
		},
	}, nil
}

func (secretLeakCanaryConnector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	secret, err := runtime.Secrets.GetSecret(ctx, "password")
	if err != nil {
		return connectors.ActionResult{}, err
	}
	payload := fmt.Sprint(action.Payload["payload"])
	wireCredential := "Basic " + base64.StdEncoding.EncodeToString([]byte("canary-user:"+secret))
	connectors.RegisterSensitiveValue(runtime.Secrets, wireCredential)
	return connectors.ActionResult{
		Status: connectors.ResultFailed,
		Output: map[string]any{
			"canary": secret,
			"nested": map[string]any{
				"password":        secret,
				"reflected_input": payload,
			},
			wireCredential: "credential-derived-key",
		},
		DisplayText: "credential=" + wireCredential + " input=" + payload,
		Error:       "canary failure credential=" + wireCredential + " input=" + payload,
	}, nil
}

func TestSecretLeakCanaryAcrossApprovalHistoryAuditAndMCP(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	runtime := fixture.server.activeRuntime()
	if err := runtime.registry.Register(secretLeakCanaryConnector{}); err != nil {
		t.Fatalf("register secret canary connector: %v", err)
	}

	store := connectortargets.NewStore(fixture.db)
	target, err := store.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: secretLeakCanaryConnectorKind,
		Name:          "secret-canary-target",
		Config:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	profile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: secretLeakCanaryConnectorKind,
		Kind: "default", Label: "main", Public: map[string]any{},
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	encryptedSecret, err := recordcrypto.EncryptJSON(
		runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profile.ID,
		map[string]any{"password": secretLeakCanaryValue},
	)
	if err != nil {
		t.Fatalf("encrypt canary credential: %v", err)
	}
	if err := store.SetCredentialProfileEncryptedSecret(ctx, target.ID, profile.ID, encryptedSecret); err != nil {
		t.Fatalf("store canary credential: %v", err)
	}
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "secret-canary-token"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID: token.ID, TargetID: target.ID, ProfileID: profile.ID,
		ActionName: "emit_secret", ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set action permission: %v", err)
	}

	wireCredential := "Basic " + base64.StdEncoding.EncodeToString([]byte("canary-user:"+secretLeakCanaryValue))
	assertCanaryAbsent := func(surface string, value string) {
		t.Helper()
		for _, canary := range []string{secretLeakCanaryValue, secretLeakInputCanaryValue, wireCredential} {
			if strings.Contains(value, canary) {
				t.Fatalf("%s leaked canary %q: %s", surface, canary, value)
			}
		}
	}
	assertRedacted := func(surface string, value string) {
		t.Helper()
		assertCanaryAbsent(surface, value)
		if !strings.Contains(value, "[REDACTED]") {
			t.Fatalf("%s did not expose a redaction marker: %s", surface, value)
		}
	}

	targetRef := connectortargets.ConnectorTargetRef(secretLeakCanaryConnectorKind, target.ID, profile.ID)
	callResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/connector-actions/call", token.TokenValue, mcpConnectorActionCallRequest{
		TargetRef: targetRef, ActionName: "emit_secret",
		Input: map[string]any{"payload": secretLeakInputCanaryValue}, Reason: "exercise secret canary boundaries",
		IdempotencyKey: "secret-canary-action",
	})
	if callResponse.Code != http.StatusOK {
		t.Fatalf("create pending canary action: %d %s", callResponse.Code, callResponse.Body.String())
	}
	assertCanaryAbsent("MCP pending response", callResponse.Body.String())
	pending := decodeRouteResponse[mcpConnectorActionResponse](t, callResponse.Body.Bytes())
	if pending.Status != string(connectors.ResultApprovalPending) || pending.RequestID < 1 {
		t.Fatalf("pending response = %#v", pending)
	}

	approvalList := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-action-approvals?status=approval_pending", "", nil)
	if approvalList.Code != http.StatusOK {
		t.Fatalf("list approvals: %d %s", approvalList.Code, approvalList.Body.String())
	}
	assertCanaryAbsent("approval list preview", approvalList.Body.String())
	approvalDetail := performJSON(
		fixture.server.Handler(), http.MethodGet,
		"/api/connector-action-approvals/"+strconv.FormatInt(pending.RequestID, 10), "", nil,
	)
	if approvalDetail.Code != http.StatusOK {
		t.Fatalf("read approval detail: %d %s", approvalDetail.Code, approvalDetail.Body.String())
	}
	assertCanaryAbsent("approval detail preview", approvalDetail.Body.String())

	runResponse := performJSON(
		fixture.server.Handler(), http.MethodPost,
		"/api/connector-action-approvals/"+strconv.FormatInt(pending.RequestID, 10)+"/run", "",
		runConnectorActionApprovalRequest{},
	)
	if runResponse.Code != http.StatusOK {
		t.Fatalf("run canary action: %d %s", runResponse.Code, runResponse.Body.String())
	}
	assertRedacted("local approval result", runResponse.Body.String())

	pollResponse := performJSON(
		fixture.server.Handler(), http.MethodGet,
		"/api/mcp/connector-action-requests/"+strconv.FormatInt(pending.RequestID, 10), token.TokenValue, nil,
	)
	if pollResponse.Code != http.StatusOK {
		t.Fatalf("poll canary action: %d %s", pollResponse.Code, pollResponse.Body.String())
	}
	assertRedacted("MCP terminal response", pollResponse.Body.String())

	historyResponse := performJSON(
		fixture.server.Handler(), http.MethodGet,
		"/api/history?connector_kind="+secretLeakCanaryConnectorKind+"&limit=10", "", nil,
	)
	if historyResponse.Code != http.StatusOK {
		t.Fatalf("list canary history: %d %s", historyResponse.Code, historyResponse.Body.String())
	}
	assertRedacted("history list", historyResponse.Body.String())
	historyPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, historyResponse.Body.Bytes())
	if len(historyPage.Items) != 1 {
		t.Fatalf("canary history items = %d", len(historyPage.Items))
	}
	historyDetail := performJSON(
		fixture.server.Handler(), http.MethodGet,
		"/api/history/"+strconv.FormatInt(historyPage.Items[0].ID, 10), "", nil,
	)
	if historyDetail.Code != http.StatusOK {
		t.Fatalf("read canary history: %d %s", historyDetail.Code, historyDetail.Body.String())
	}
	assertRedacted("history detail", historyDetail.Body.String())

	auditResponse := performJSON(
		fixture.server.Handler(), http.MethodGet,
		"/api/audit-logs?connector_kind="+secretLeakCanaryConnectorKind+"&target_id="+strconv.FormatInt(target.ID, 10), "", nil,
	)
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("list canary audit: %d %s", auditResponse.Code, auditResponse.Body.String())
	}
	assertCanaryAbsent("audit list", auditResponse.Body.String())
	auditPage := decodeRouteResponse[pageResponse[auditLogRecord]](t, auditResponse.Body.Bytes())
	if len(auditPage.Items) == 0 {
		t.Fatal("expected canary audit events")
	}
	for _, item := range auditPage.Items {
		detail := performJSON(
			fixture.server.Handler(), http.MethodGet,
			"/api/audit-logs/"+strconv.FormatInt(item.ID, 10), "", nil,
		)
		if detail.Code != http.StatusOK {
			t.Fatalf("read canary audit %d: %d %s", item.ID, detail.Code, detail.Body.String())
		}
		assertCanaryAbsent("audit detail", detail.Body.String())
	}

	for _, canary := range []string{secretLeakCanaryValue, secretLeakInputCanaryValue, wireCredential} {
		var plaintextReferences int
		if err := fixture.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM connector_action_requests
			 WHERE preview_json LIKE ? OR input_json LIKE ? OR output_json LIKE ? OR display_text LIKE ? OR error LIKE ?)
			+
			(SELECT COUNT(*) FROM history_entries
			 WHERE preview_json LIKE ? OR input_text LIKE ? OR input_json LIKE ? OR output_text LIKE ? OR output_json LIKE ? OR error LIKE ?)
			+
			(SELECT COUNT(*) FROM audit_logs WHERE payload_json LIKE ?)`,
			"%"+canary+"%", "%"+canary+"%", "%"+canary+"%", "%"+canary+"%", "%"+canary+"%",
			"%"+canary+"%", "%"+canary+"%", "%"+canary+"%", "%"+canary+"%", "%"+canary+"%", "%"+canary+"%",
			"%"+canary+"%",
		).Scan(&plaintextReferences); err != nil {
			t.Fatalf("inspect plaintext persistence surfaces for %q: %v", canary, err)
		}
		if plaintextReferences != 0 {
			t.Fatalf("canary %q appeared in %d plaintext persistence surfaces", canary, plaintextReferences)
		}
	}
}
