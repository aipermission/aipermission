package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestRedactBasicMasksCommonSecretShapes(t *testing.T) {
	input := strings.Join([]string{
		"password=super-secret",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456",
		"token: ghp_abcdefghijklmnopqrstuvwxyz123456",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----",
	}, "\n")
	output := redactBasic(input)
	for _, secret := range []string{"super-secret", "abcdefghijklmnopqrstuvwxyz123456", "abc\n-----END"} {
		if strings.Contains(output, secret) {
			t.Fatalf("secret fragment %q was not redacted: %s", secret, output)
		}
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Fatalf("expected redaction marker: %s", output)
	}
}

func TestRedactBasicKeepsShellPWDOutput(t *testing.T) {
	output := redactBasic("PWD=/home/hakan/workspace\npwd=super-secret\nPASSWORD=another-secret")
	if !strings.Contains(output, "PWD=/home/hakan/workspace") {
		t.Fatalf("shell PWD output should not be redacted: %s", output)
	}
	for _, secret := range []string{"super-secret", "another-secret"} {
		if strings.Contains(output, secret) {
			t.Fatalf("secret %q was not redacted: %s", secret, output)
		}
	}
}

func TestCustomRedactionRulesApplyOnlyInBasicMode(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	if _, err := insertRedactionRule(t.Context(), runtime, redactionRuleRequest{
		Name:    "internal token",
		Pattern: `internal_[a-z0-9]+`,
		Enabled: true,
	}); err != nil {
		t.Fatalf("insert custom rule: %v", err)
	}

	redacted := fixture.server.redactForPersistence(t.Context(), runtime, "value=internal_abc123")
	if strings.Contains(redacted, "internal_abc123") || !strings.Contains(redacted, "[REDACTED]") {
		t.Fatalf("custom rule should redact in basic mode: %s", redacted)
	}

	if err := writeSecuritySettings(t.Context(), runtime, securitySettingsResponse{RedactionMode: redactionModeOff}); err != nil {
		t.Fatalf("disable redaction: %v", err)
	}
	unredacted := fixture.server.redactForPersistence(t.Context(), runtime, "value=internal_abc123")
	if unredacted != "value=internal_abc123" {
		t.Fatalf("redaction off should leave value unchanged: %s", unredacted)
	}
}

func TestRedactionRuleEndpointsValidateAndPersistRules(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()

	response := performJSON(handler, http.MethodPost, "/api/settings/redaction-rules", "", redactionRuleRequest{
		Name:    "bad",
		Pattern: "[",
		Enabled: true,
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid regex should fail, got %d %s", response.Code, response.Body.String())
	}

	response = performJSON(handler, http.MethodPost, "/api/settings/redaction-rules", "", redactionRuleRequest{
		Name:    "internal",
		Pattern: `internal_[a-z0-9]+`,
		Enabled: true,
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create rule failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/api/settings/redaction-rules", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "internal") {
		t.Fatalf("list rules failed: %d %s", response.Code, response.Body.String())
	}
}

func TestCommandRequestKeepsEncryptedRawCommandForExecution(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	if _, err := fixture.tokens.UpdatePermissions(ctx, token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleApprovalRequired},
	}}); err != nil {
		t.Fatalf("update permissions: %v", err)
	}
	runtime := fixture.server.activeRuntime()

	rawCommand := "curl -H 'Authorization: Bearer secret-token-1234567890' https://example.invalid"
	id, err := fixture.server.insertCommandRequest(ctx, runtime, token.ID, server.ID, rawCommand, "password=secret-value", "pending_approval")
	if err != nil {
		t.Fatalf("insert command request: %v", err)
	}

	record, err := fixture.server.getCommandRequest(ctx, runtime, id, token.ID, commandRequestSourceMCP)
	if err != nil {
		t.Fatalf("get command request: %v", err)
	}
	if strings.Contains(record.Command, "secret-token-1234567890") || strings.Contains(record.Reason, "secret-value") {
		t.Fatalf("display fields should be redacted: %#v", record)
	}
	executionCommand, err := fixture.server.commandRequestExecutionCommand(ctx, runtime, id)
	if err != nil {
		t.Fatalf("read execution command: %v", err)
	}
	if executionCommand != rawCommand {
		t.Fatalf("execution command changed: got %q want %q", executionCommand, rawCommand)
	}
}

func TestCommandRequestErrorsAreRedactedBeforePersistence(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	runtime := fixture.server.activeRuntime()
	id, err := fixture.server.insertCommandRequest(ctx, runtime, token.ID, server.ID, "echo ok", "test", "running")
	if err != nil {
		t.Fatalf("insert command request: %v", err)
	}
	if err := fixture.server.finishCommandRequest(ctx, runtime, id, "error", 0, "", "", 1, "ssh failed password=super-secret"); err != nil {
		t.Fatalf("finish command request: %v", err)
	}
	record, err := fixture.server.getCommandRequest(ctx, runtime, id, token.ID, commandRequestSourceMCP)
	if err != nil {
		t.Fatalf("get command request: %v", err)
	}
	if strings.Contains(record.Error, "super-secret") || !strings.Contains(record.Error, "[REDACTED]") {
		t.Fatalf("error should be redacted before persistence: %#v", record)
	}
}

func TestRedactionRuleCacheInvalidatesOnUpdate(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	item, err := insertRedactionRule(t.Context(), runtime, redactionRuleRequest{
		Name:    "first",
		Pattern: `alpha_[a-z0-9]+`,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("insert custom rule: %v", err)
	}
	if redacted := fixture.server.redactForPersistence(t.Context(), runtime, "value=alpha_secret"); strings.Contains(redacted, "alpha_secret") {
		t.Fatalf("expected first custom rule to redact: %s", redacted)
	}

	if _, err := updateRedactionRuleRecord(t.Context(), runtime, item.ID, redactionRuleRequest{
		Name:    "second",
		Pattern: `beta_[a-z0-9]+`,
		Enabled: true,
	}); err != nil {
		t.Fatalf("update custom rule: %v", err)
	}
	fixture.server.invalidateRedactionRules(runtime)
	redacted := fixture.server.redactForPersistence(t.Context(), runtime, "value=alpha_secret beta_secret")
	if !strings.Contains(redacted, "alpha_secret") || strings.Contains(redacted, "beta_secret") {
		t.Fatalf("expected cache refresh to use updated rule: %s", redacted)
	}
}
