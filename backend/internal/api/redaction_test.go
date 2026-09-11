package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestCustomRedactionRulesApplyOnlyInBasicMode(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	if _, err := createSecurityPolicyRule(t.Context(), runtime, securitypolicy.RuleInput{
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

	if err := setSecurityPolicySettings(t.Context(), runtime, securitypolicy.Settings{RedactionMode: securitypolicy.RedactionModeOff}); err != nil {
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

	response := performJSON(handler, http.MethodPost, "/api/settings/redaction-rules", "", securitypolicy.RuleInput{
		Name:    "bad",
		Pattern: "[",
		Enabled: true,
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid regex should fail, got %d %s", response.Code, response.Body.String())
	}

	response = performJSON(handler, http.MethodPost, "/api/settings/redaction-rules", "", securitypolicy.RuleInput{
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
	runtime := fixture.server.activeRuntime()

	rawCommand := "curl -H 'Authorization: Bearer secret-token-1234567890' https://example.invalid"
	requests := requireCommandRuntime(t, fixture.server, runtime)
	id, err := requests.Insert(ctx, commandrequests.Insert{
		TokenID: &token.ID, RuntimeID: server.ID, Source: commandrequests.SourceMCP,
		Command: rawCommand, Reason: "password=secret-value", Status: "pending_approval",
	})
	if err != nil {
		t.Fatalf("insert command request: %v", err)
	}

	record, err := requests.Get(ctx, id, token.ID, commandRequestSourceMCP)
	if err != nil {
		t.Fatalf("get command request: %v", err)
	}
	if strings.Contains(record.Command, "secret-token-1234567890") || strings.Contains(record.Reason, "secret-value") {
		t.Fatalf("display fields should be redacted: %#v", record)
	}
	executionCommand, err := requests.ExecutionCommand(ctx, id)
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
	requests := requireCommandRuntime(t, fixture.server, runtime)
	id, err := requests.Insert(ctx, commandrequests.Insert{
		TokenID: &token.ID, RuntimeID: server.ID, Source: commandrequests.SourceMCP,
		Command: "echo ok", Reason: "test", Status: "running",
	})
	if err != nil {
		t.Fatalf("insert command request: %v", err)
	}
	if err := requests.Finish(ctx, commandrequests.Completion{
		ID: id, Status: "error", ExitCode: 1, Error: "ssh failed password=super-secret",
	}); err != nil {
		t.Fatalf("finish command request: %v", err)
	}
	record, err := requests.Get(ctx, id, token.ID, commandRequestSourceMCP)
	if err != nil {
		t.Fatalf("get command request: %v", err)
	}
	if strings.Contains(record.Error, "super-secret") || !strings.Contains(record.Error, "[REDACTED]") {
		t.Fatalf("error should be redacted before persistence: %#v", record)
	}
}

func TestConnectorInputRedactionRemovesLargeSensitivePayloadBeforeProjectionLimits(t *testing.T) {
	fixture := newAPITestFixture(t)
	largeUpload := strings.Repeat("s", 2<<20)
	redacted, err := fixture.server.redactConnectorActionInput(t.Context(), fixture.server.activeRuntime(), map[string]any{
		"key":          "artifact.txt",
		"content_text": largeUpload,
	}, []string{"content_text"})
	if err != nil {
		t.Fatalf("redact large connector input: %v", err)
	}
	if redacted["content_text"] != "[REDACTED]" || redacted["key"] != "artifact.txt" {
		t.Fatalf("unexpected redacted connector input: %#v", redacted)
	}
	if _, err := fixture.server.redactConnectorActionInput(t.Context(), fixture.server.activeRuntime(), map[string]any{
		"content_text": largeUpload,
	}, nil); err == nil {
		t.Fatal("large non-sensitive projection should retain strict display limits")
	}
}
