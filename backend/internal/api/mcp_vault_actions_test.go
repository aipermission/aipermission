package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/projectcapabilities"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

func TestMCPVaultListReportsExactTruncationAtProjectBoundary(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	projectStore := projectstore.NewStore(fixture.db)
	fullProject, err := projectStore.Create(ctx, "A Full Vault")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projectStore.Create(ctx, "Z Empty Vault"); err != nil {
		t.Fatal(err)
	}
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "vault-list-boundary"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projectcapabilities.NewStore(fixture.db).Replace(ctx, token.ID, []projectcapabilities.SetInput{{
		ProjectID: fullProject.ID, Name: projectcapabilities.VaultMetadataRead,
		ExecutionRule: projectcapabilities.RuleAlwaysRun,
	}}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxMCPVaultItems; index++ {
		if _, err := fixture.db.ExecContext(ctx, `
			INSERT INTO vault_items (
				name, owner_project_id, secret_type, last_value_replaced_at, source, created_at, updated_at
			)
			VALUES (?, ?, 'generic_secret', datetime('now'), 'imported', datetime('now'), datetime('now'))`,
			fmt.Sprintf("BOUNDARY_SECRET_%03d", index),
			fullProject.ID,
		); err != nil {
			t.Fatalf("insert Vault item %d: %v", index, err)
		}
	}

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/vault-items", token.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list Vault items: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Count     int  `json:"count"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != maxMCPVaultItems || body.Truncated {
		t.Fatalf("boundary list count=%d truncated=%v", body.Count, body.Truncated)
	}
}

func TestMCPVaultGenerateRequiresLocalApprovalAndNeverReturnsSecret(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	project, err := projectstore.NewStore(fixture.db).Create(ctx, "Vault Agent Project")
	if err != nil {
		t.Fatal(err)
	}
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "vault-codex"})
	if err != nil {
		t.Fatal(err)
	}
	otherToken, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "other-codex"})
	if err != nil {
		t.Fatal(err)
	}
	capabilityPath := "/api/tokens/" + strconv.FormatInt(token.ID, 10) + "/project-capabilities"
	capabilities := performJSON(fixture.server.Handler(), http.MethodPut, capabilityPath, "", updateProjectCapabilitiesRequest{
		Capabilities: []projectCapabilityInput{
			{ProjectID: project.ID, CapabilityName: projectcapabilities.VaultMetadataRead, ExecutionRule: projectcapabilities.RuleAlwaysRun},
			{ProjectID: project.ID, CapabilityName: projectcapabilities.VaultItemGenerate, ExecutionRule: projectcapabilities.RuleApprovalRequired},
		},
	})
	if capabilities.Code != http.StatusOK {
		t.Fatalf("set Vault capabilities: %d %s", capabilities.Code, capabilities.Body.String())
	}
	rejected := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-actions/call", token.TokenValue, mcpVaultActionCallRequest{
		ProjectRef: project.Slug, ActionName: vaultrequests.ActionGenerateItem,
		Input: map[string]any{
			"name": "REJECTED_SECRET", "generator_kind": "random_token",
			"value": "must-never-be-persisted",
		},
		Reason: "Verify strict Vault input validation.", IdempotencyKey: "rejected-secret-input",
	})
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("unknown Vault input field = %d %s", rejected.Code, rejected.Body.String())
	}
	var rejectedCount int
	if err := fixture.db.QueryRow(
		`SELECT COUNT(*) FROM vault_action_requests WHERE idempotency_key = ?`,
		"rejected-secret-input",
	).Scan(&rejectedCount); err != nil {
		t.Fatal(err)
	}
	if rejectedCount != 0 {
		t.Fatal("rejected Vault input was persisted")
	}
	for index, nestedInput := range []map[string]any{
		{
			"name": "REJECTED_USAGE_NOTE", "generator_kind": "random_token",
			"usage_notes": []any{map[string]any{
				"location": "service.env", "notes": "non-secret", "created_at": "forged",
			}},
		},
		{
			"target_ref": "ssh:1:1",
			"items": []any{map[string]any{
				"item_id": 1, "source_project_id": 1, "binding_revision": 7,
			}},
		},
	} {
		actionName := vaultrequests.ActionGenerateItem
		if index == 1 {
			actionName = vaultrequests.ActionRestartSession
		}
		response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-actions/call", token.TokenValue, mcpVaultActionCallRequest{
			ProjectRef: project.Slug, ActionName: actionName, Input: nestedInput,
			Reason: "Verify nested strict input validation.", IdempotencyKey: "rejected-nested-" + strconv.Itoa(index),
		})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unknown nested Vault input field %d = %d %s", index, response.Code, response.Body.String())
		}
	}

	input := map[string]any{
		"name": "MY_PROJECT_API_TOKEN", "secret_type": "api_key",
		"generator_kind": "random_token", "provider": "Example API",
		"description": "Generated without exposing its value.",
	}
	callBody := mcpVaultActionCallRequest{
		ProjectRef: project.Slug, ActionName: vaultrequests.ActionGenerateItem,
		Input: input, Reason: "Create the project API token.",
		IdempotencyKey: "generate-project-token-1",
	}
	call := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-actions/call", token.TokenValue, callBody)
	if call.Code != http.StatusOK || !strings.Contains(call.Body.String(), `"status":"approval_pending"`) {
		t.Fatalf("call Vault action: %d %s", call.Code, call.Body.String())
	}
	var pending map[string]any
	if err := json.Unmarshal(call.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	requestID := int64(pending["request_id"].(float64))

	if _, err := fixture.db.Exec(`
		UPDATE token_project_capabilities
		SET expires_at = ?, updated_at = ?
		WHERE token_id = ? AND project_id = ? AND capability_name = ?`,
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
		token.ID,
		project.ID,
		projectcapabilities.VaultItemGenerate,
	); err != nil {
		t.Fatalf("expire capability before idempotent retry: %v", err)
	}
	repeated := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-actions/call", token.TokenValue, callBody)
	if repeated.Code != http.StatusOK || !strings.Contains(repeated.Body.String(), `"request_id":`+strconv.FormatInt(requestID, 10)) {
		t.Fatalf("idempotent Vault action after context drift: %d %s", repeated.Code, repeated.Body.String())
	}
	conflictingBody := callBody
	conflictingBody.Reason = "Reuse the same key for a different request."
	conflicting := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-actions/call", token.TokenValue, conflictingBody)
	if conflicting.Code != http.StatusConflict {
		t.Fatalf("conflicting idempotent Vault action: %d %s", conflicting.Code, conflicting.Body.String())
	}
	if _, err := fixture.db.Exec(`
		UPDATE token_project_capabilities
		SET expires_at = NULL, updated_at = ?
		WHERE token_id = ? AND project_id = ? AND capability_name = ?`,
		time.Now().UTC().Format(time.RFC3339),
		token.ID,
		project.ID,
		projectcapabilities.VaultItemGenerate,
	); err != nil {
		t.Fatalf("restore capability after idempotent retry: %v", err)
	}
	foreignPoll := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/vault-action-requests/"+strconv.FormatInt(requestID, 10), otherToken.TokenValue, nil)
	if foreignPoll.Code != http.StatusNotFound {
		t.Fatalf("foreign token poll = %d %s", foreignPoll.Code, foreignPoll.Body.String())
	}
	foreignCancel := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-action-requests/"+strconv.FormatInt(requestID, 10)+"/cancel", otherToken.TokenValue, map[string]any{})
	if foreignCancel.Code != http.StatusNotFound {
		t.Fatalf("foreign token cancel = %d %s", foreignCancel.Code, foreignCancel.Body.String())
	}

	run := performJSON(fixture.server.Handler(), http.MethodPost, "/api/vault-action-approvals/"+strconv.FormatInt(requestID, 10)+"/run", "", vaultActionDecisionRequest{UserNote: "Approved locally."})
	if run.Code != http.StatusOK || strings.Contains(run.Body.String(), `"value"`) || strings.Contains(run.Body.String(), "encrypted_value") {
		t.Fatalf("run Vault action: %d %s", run.Code, run.Body.String())
	}
	poll := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/vault-action-requests/"+strconv.FormatInt(requestID, 10), token.TokenValue, nil)
	if poll.Code != http.StatusOK || !strings.Contains(poll.Body.String(), `"status":"completed"`) ||
		strings.Contains(poll.Body.String(), `"value"`) || strings.Contains(poll.Body.String(), "encrypted_value") {
		t.Fatalf("poll Vault action: %d %s", poll.Code, poll.Body.String())
	}
	list := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/vault-items?project_ref="+project.Slug, token.TokenValue, nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "MY_PROJECT_API_TOKEN") ||
		!strings.Contains(list.Body.String(), `"secret_values_returned":false`) {
		t.Fatalf("list Vault metadata: %d %s", list.Code, list.Body.String())
	}
	for _, forbidden := range []string{`"value"`, `"encrypted_value"`, `"description"`, `"usage_notes"`, `"provider"`, `"environment"`} {
		if strings.Contains(list.Body.String(), forbidden) {
			t.Fatalf("list Vault metadata exposed forbidden field %s: %s", forbidden, list.Body.String())
		}
	}

	cancelBody := callBody
	cancelBody.IdempotencyKey = "generate-project-token-cancel"
	cancelBody.Input = map[string]any{
		"name": "CANCELED_PROJECT_TOKEN", "secret_type": "api_key",
		"generator_kind": "random_token",
	}
	cancelCall := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-actions/call", token.TokenValue, cancelBody)
	if cancelCall.Code != http.StatusOK {
		t.Fatalf("call cancelable Vault action: %d %s", cancelCall.Code, cancelCall.Body.String())
	}
	var cancelPending map[string]any
	if err := json.Unmarshal(cancelCall.Body.Bytes(), &cancelPending); err != nil {
		t.Fatal(err)
	}
	cancelID := int64(cancelPending["request_id"].(float64))
	cancel := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-action-requests/"+strconv.FormatInt(cancelID, 10)+"/cancel", token.TokenValue, map[string]any{})
	if cancel.Code != http.StatusOK || !strings.Contains(cancel.Body.String(), `"status":"canceled"`) ||
		strings.Contains(cancel.Body.String(), `"retry_after_seconds"`) {
		t.Fatalf("cancel Vault action: %d %s", cancel.Code, cancel.Body.String())
	}

	revokedCapabilities := performJSON(fixture.server.Handler(), http.MethodPut, capabilityPath, "", updateProjectCapabilitiesRequest{
		Capabilities: []projectCapabilityInput{
			{ProjectID: project.ID, CapabilityName: projectcapabilities.VaultMetadataRead, ExecutionRule: projectcapabilities.RuleAlwaysRun},
		},
	})
	if revokedCapabilities.Code != http.StatusOK {
		t.Fatalf("revoke Vault generation capability: %d %s", revokedCapabilities.Code, revokedCapabilities.Body.String())
	}
	withheld := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/vault-action-requests/"+strconv.FormatInt(requestID, 10), token.TokenValue, nil)
	if withheld.Code != http.StatusOK || !strings.Contains(withheld.Body.String(), `"output_withheld":true`) ||
		strings.Contains(withheld.Body.String(), `"output":`) {
		t.Fatalf("revoked capability should withhold terminal output: %d %s", withheld.Code, withheld.Body.String())
	}
}

func TestMCPVaultGenerateAlwaysRunsWithoutReturningSecret(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	project, err := projectstore.NewStore(fixture.db).Create(ctx, "Autonomous Vault Project")
	if err != nil {
		t.Fatal(err)
	}
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "vault-automation"})
	if err != nil {
		t.Fatal(err)
	}
	capabilities := performJSON(fixture.server.Handler(), http.MethodPut,
		"/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/project-capabilities", "",
		updateProjectCapabilitiesRequest{Capabilities: []projectCapabilityInput{
			{ProjectID: project.ID, CapabilityName: projectcapabilities.VaultMetadataRead, ExecutionRule: projectcapabilities.RuleAlwaysRun},
			{ProjectID: project.ID, CapabilityName: projectcapabilities.VaultItemGenerate, ExecutionRule: projectcapabilities.RuleAlwaysRun},
		}},
	)
	if capabilities.Code != http.StatusOK {
		t.Fatalf("set always Vault capabilities: %d %s", capabilities.Code, capabilities.Body.String())
	}

	callBody := mcpVaultActionCallRequest{
		ProjectRef: project.Slug,
		ActionName: vaultrequests.ActionGenerateItem,
		Input: map[string]any{
			"name": "AUTOMATED_API_TOKEN", "secret_type": "api_key",
			"generator_kind": "random_token", "provider": "Example API",
		},
		Reason:         "Create a token for the approved autonomous workflow.",
		IdempotencyKey: "always-generate-token-1",
	}
	fixture.server.vaultRequestLimiter = newWindowRateLimiter(1, time.Minute)
	call := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-actions/call", token.TokenValue, callBody)
	if call.Code != http.StatusOK || !strings.Contains(call.Body.String(), `"status":"completed"`) ||
		strings.Contains(call.Body.String(), `"retry_after_seconds"`) ||
		strings.Contains(call.Body.String(), `"value"`) || strings.Contains(call.Body.String(), "encrypted_value") {
		t.Fatalf("always Vault action: %d %s", call.Code, call.Body.String())
	}
	var completed map[string]any
	if err := json.Unmarshal(call.Body.Bytes(), &completed); err != nil {
		t.Fatal(err)
	}
	requestID := int64(completed["request_id"].(float64))

	repeated := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-actions/call", token.TokenValue, callBody)
	if repeated.Code != http.StatusOK ||
		!strings.Contains(repeated.Body.String(), `"request_id":`+strconv.FormatInt(requestID, 10)) ||
		!strings.Contains(repeated.Body.String(), `"status":"completed"`) {
		t.Fatalf("idempotent always Vault action: %d %s", repeated.Code, repeated.Body.String())
	}
	limitedBody := callBody
	limitedBody.IdempotencyKey = "always-generate-token-2"
	limitedBody.Input = map[string]any{
		"name": "RATE_LIMITED_API_TOKEN", "secret_type": "api_key",
		"generator_kind": "random_token",
	}
	limited := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-actions/call", token.TokenValue, limitedBody)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("new Vault request should be rate limited: %d %s", limited.Code, limited.Body.String())
	}
	var itemCount int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM vault_items WHERE name = 'AUTOMATED_API_TOKEN'`).Scan(&itemCount); err != nil {
		t.Fatal(err)
	}
	if itemCount != 1 {
		t.Fatalf("generated Vault item count = %d", itemCount)
	}
	var startAudits, completeAudits int
	if err := fixture.db.QueryRow(`
		SELECT
			SUM(CASE WHEN action = 'mcp.vault_action.always_run_requested' THEN 1 ELSE 0 END),
			SUM(CASE WHEN action = 'mcp.vault_action.completed' THEN 1 ELSE 0 END)
		FROM audit_logs
		WHERE token_id = ?`, token.ID,
	).Scan(&startAudits, &completeAudits); err != nil {
		t.Fatal(err)
	}
	if startAudits != 1 || completeAudits != 1 {
		t.Fatalf("always Vault audit counts = start:%d completed:%d", startAudits, completeAudits)
	}
}

func TestMCPVaultSessionApplyPromptAlwaysAndHumanIsolation(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	runtime := fixture.server.activeRuntime()
	project, err := projectstore.NewStore(fixture.db).Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	target := fixture.createKeyAndServer(t, "vault-session-e2e")
	fixture.trustServerHostKey(t, target)
	identities, err := execution.TrustedHostFingerprints(
		fixture.server.ConnectorTrustStorePath(),
		net.JoinHostPort(target.Host, strconv.Itoa(target.Port)),
	)
	if err != nil || len(identities) != 1 {
		t.Fatalf("trusted peer identity: %#v %v", identities, err)
	}
	const secretValue = "vault-session-secret-must-never-persist-123"
	createItem := performJSON(fixture.server.Handler(), http.MethodPost, "/api/vault-items", "", createVaultItemRequest{
		Name: "SESSION_E2E_TOKEN", Value: secretValue,
		OwnerProjectID: project.ID, SecretType: "api_key", Source: "imported",
	})
	if createItem.Code != http.StatusCreated {
		t.Fatalf("create Vault item: %d %s", createItem.Code, createItem.Body.String())
	}
	item := decodeRouteResponse[projectvault.Item](t, createItem.Body.Bytes())
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "vault-session-e2e-token"})
	if err != nil {
		t.Fatal(err)
	}
	if response := performJSON(
		fixture.server.Handler(),
		http.MethodPut,
		"/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/project-scopes",
		"",
		updateTokenProjectScopesRequest{EnabledProjectIDs: []int64{project.ID}},
	); response.Code != http.StatusOK {
		t.Fatalf("set project scope: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(
		fixture.server.Handler(),
		http.MethodPut,
		"/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/project-capabilities",
		"",
		updateProjectCapabilitiesRequest{Capabilities: []projectCapabilityInput{{
			ProjectID: project.ID, CapabilityName: projectcapabilities.VaultSessionApply,
			ExecutionRule: projectcapabilities.RuleAlwaysRun,
		}}},
	); response.Code != http.StatusOK {
		t.Fatalf("set Vault capability: %d %s", response.Code, response.Body.String())
	}
	permissions := connectortargets.NewStore(fixture.db)
	setPermission := func(rule connectortargets.ActionPermissionRule) {
		t.Helper()
		if _, err := permissions.ReplaceActionPermissions(ctx, token.ID, []connectortargets.SetActionPermissionInput{{
			TargetID: target.TargetID, ProfileID: target.ProfileID,
			ActionName: sshconnector.ActionExec, ExecutionRule: rule,
		}}); err != nil {
			t.Fatalf("set connector permission: %v", err)
		}
	}
	setPermission(connectortargets.ActionPermissionAlwaysRun)

	var appliedValues []string
	var openedGeometry [][2]int
	runtime.consoleSessions = console.NewManager(fixture.db, func(openCtx context.Context, request console.RuntimeOpenRequest) (*console.RuntimeSession, error) {
		openedGeometry = append(openedGeometry, [2]int{request.Cols, request.Rows})
		return &console.RuntimeSession{
			Stdin:        discardWriteCloser{},
			Stdout:       strings.NewReader(""),
			PeerIdentity: identities[0],
			ApplyEnvironment: func(_ context.Context, envelope *sessionenv.Envelope) error {
				return envelope.WithEntries(func(entries []sessionenv.EntryView) error {
					for _, entry := range entries {
						appliedValues = append(appliedValues, string(entry.Value))
					}
					return nil
				})
			},
			Wait: func() error {
				<-openCtx.Done()
				return openCtx.Err()
			},
			Close: func() error { return nil },
		}, nil
	}, fixture.server.runtimeRedactor(runtime))
	fixture.server.configureVaultSessionRuntime(runtime)

	callBody := mcpVaultActionCallRequest{
		ProjectRef: project.Slug, ActionName: vaultrequests.ActionRestartSession,
		Input: map[string]any{
			"target_ref": target.TargetRef,
			"items": []any{map[string]any{
				"item_id": item.ID, "source_project_id": project.ID,
			}},
		},
		Reason: "Start the approved session with its Vault environment.",
	}
	callBody.IdempotencyKey = "vault-session-e2e-always"
	always := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-actions/call", token.TokenValue, callBody)
	if always.Code != http.StatusOK || !strings.Contains(always.Body.String(), `"status":"completed"`) {
		t.Fatalf("Always session apply: %d %s", always.Code, always.Body.String())
	}
	var alwaysResponse map[string]any
	if err := json.Unmarshal(always.Body.Bytes(), &alwaysResponse); err != nil {
		t.Fatal(err)
	}
	alwaysOutput := alwaysResponse["output"].(map[string]any)
	alwaysSessionID := int64(alwaysOutput["session_id"].(float64))
	alwaysGeneration := int64(alwaysOutput["session_generation"].(float64))
	if !vaultSessionObserveAuthorized(ctx, runtime, token.ID, alwaysSessionID, alwaysGeneration, target.ID, true) {
		t.Fatal("Always session was not authorized for the owning token")
	}
	runtime.consoleSessions.Resize(alwaysSessionID, 141, 47)

	setPermission(connectortargets.ActionPermissionApprovalRequired)
	callBody.IdempotencyKey = "vault-session-e2e-prompt"
	prompt := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/vault-actions/call", token.TokenValue, callBody)
	if prompt.Code != http.StatusOK || !strings.Contains(prompt.Body.String(), `"status":"approval_pending"`) {
		t.Fatalf("Prompt session apply: %d %s", prompt.Code, prompt.Body.String())
	}
	var pending map[string]any
	if err := json.Unmarshal(prompt.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	requestID := int64(pending["request_id"].(float64))
	run := performJSON(
		fixture.server.Handler(),
		http.MethodPost,
		"/api/vault-action-approvals/"+strconv.FormatInt(requestID, 10)+"/run",
		"",
		vaultActionDecisionRequest{UserNote: "Approved locally."},
	)
	if run.Code != http.StatusOK || !strings.Contains(run.Body.String(), `"status":"completed"`) {
		t.Fatalf("run Prompt session apply: %d %s", run.Code, run.Body.String())
	}
	if len(openedGeometry) != 2 || openedGeometry[1] != [2]int{141, 47} {
		t.Fatalf("replacement geometry was not inherited: %#v", openedGeometry)
	}
	if len(appliedValues) != 2 || appliedValues[0] != secretValue || appliedValues[1] != secretValue {
		t.Fatalf("fake transport did not receive the expected secret twice: %#v", appliedValues)
	}

	human := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/sessions", "", createConsoleSessionRequest{
		RuntimeID: target.ID, Name: "Local Vault session", CloseExisting: true,
		Cols: 100, Rows: 30,
		VaultItems: []projectvault.SessionSelection{{
			ItemID: item.ID, SourceProjectID: project.ID,
		}},
	})
	if human.Code != http.StatusCreated {
		t.Fatalf("create human Vault session: %d %s", human.Code, human.Body.String())
	}
	humanRecord := decodeRouteResponse[console.Record](t, human.Body.Bytes())
	if vaultSessionObserveAuthorized(ctx, runtime, token.ID, humanRecord.ID, humanRecord.Generation, target.ID, true) {
		t.Fatal("MCP token unexpectedly received access to a human Vault session")
	}

	var persisted string
	if err := fixture.db.QueryRow(`
		SELECT
			COALESCE(group_concat(payload_json, ''), '') ||
			COALESCE((SELECT group_concat(input_json || output_json || error, '') FROM vault_action_requests), '') ||
			COALESCE((SELECT group_concat(input_text || input_json || output_text || output_json || error, '') FROM history_entries), '') ||
			COALESCE((SELECT group_concat(transcript || error, '') FROM console_sessions), '')
		FROM audit_logs`,
	).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(persisted, secretValue) ||
		strings.Contains(always.Body.String(), secretValue) ||
		strings.Contains(run.Body.String(), secretValue) {
		t.Fatal("Vault secret leaked into a persisted or MCP response surface")
	}
}

type discardWriteCloser struct{}

func (discardWriteCloser) Write(value []byte) (int, error) { return len(value), nil }
func (discardWriteCloser) Close() error                    { return nil }

func TestVaultSessionContextAcceptsAlwaysCapability(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	project, err := projectstore.NewStore(fixture.db).Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	target := fixture.createKeyAndServer(t, "always-vault-session")
	fixture.trustServerHostKey(t, target)
	createItem := performJSON(fixture.server.Handler(), http.MethodPost, "/api/vault-items", "", createVaultItemRequest{
		Name: "SESSION_API_TOKEN", Value: "session-secret-value",
		OwnerProjectID: project.ID, SecretType: "api_key", Source: "imported",
	})
	if createItem.Code != http.StatusCreated {
		t.Fatalf("create Vault item: %d %s", createItem.Code, createItem.Body.String())
	}
	item := decodeRouteResponse[projectvault.Item](t, createItem.Body.Bytes())
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "vault-session-automation"})
	if err != nil {
		t.Fatal(err)
	}
	scope := performJSON(
		fixture.server.Handler(),
		http.MethodPut,
		"/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/project-scopes",
		"",
		updateTokenProjectScopesRequest{EnabledProjectIDs: []int64{project.ID}},
	)
	if scope.Code != http.StatusOK {
		t.Fatalf("set project scope: %d %s", scope.Code, scope.Body.String())
	}
	capabilities := performJSON(
		fixture.server.Handler(),
		http.MethodPut,
		"/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/project-capabilities",
		"",
		updateProjectCapabilitiesRequest{Capabilities: []projectCapabilityInput{{
			ProjectID: project.ID, CapabilityName: projectcapabilities.VaultSessionApply,
			ExecutionRule: projectcapabilities.RuleAlwaysRun,
		}}},
	)
	if capabilities.Code != http.StatusOK {
		t.Fatalf("set Always session capability: %d %s", capabilities.Code, capabilities.Body.String())
	}
	if _, err := connectortargets.NewStore(fixture.db).ReplaceActionPermissions(ctx, token.ID, []connectortargets.SetActionPermissionInput{{
		TargetID:      target.TargetID,
		ProfileID:     target.ProfileID,
		ActionName:    sshconnector.ActionExec,
		ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}}); err != nil {
		t.Fatalf("set Always connector permission: %v", err)
	}

	approval, _, _, err := buildVaultApprovalContext(
		ctx,
		fixture.server,
		fixture.server.activeRuntime(),
		token.ID,
		project,
		vaultrequests.ActionRestartSession,
		map[string]any{
			"target_ref": target.TargetRef,
			"items": []any{map[string]any{
				"item_id": item.ID, "source_project_id": project.ID,
			}},
		},
	)
	if err != nil {
		t.Fatalf("build Always session context: %v", err)
	}
	if approval.ExecutionRule != projectcapabilities.RuleAlwaysRun ||
		approval.RuntimeID != target.ID || len(approval.Items) != 1 {
		t.Fatalf("unexpected Always session context: %#v", approval)
	}

	if _, err := connectortargets.NewStore(fixture.db).ReplaceActionPermissions(ctx, token.ID, []connectortargets.SetActionPermissionInput{{
		TargetID:      target.TargetID,
		ProfileID:     target.ProfileID,
		ActionName:    sshconnector.ActionExec,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}}); err != nil {
		t.Fatalf("change connector permission to Prompt: %v", err)
	}
	if _, err := validateVaultApprovalAuthorization(ctx, fixture.server, fixture.server.activeRuntime(), vaultrequests.Request{
		TokenID: token.ID, ProjectID: project.ID, ActionName: vaultrequests.ActionRestartSession,
	}, approval); !isVaultContextDrift(err) {
		t.Fatalf("connector permission drift should stale the approval, got %v", err)
	}
	promptApproval, _, _, err := buildVaultApprovalContext(
		ctx,
		fixture.server,
		fixture.server.activeRuntime(),
		token.ID,
		project,
		vaultrequests.ActionRestartSession,
		map[string]any{
			"target_ref": target.TargetRef,
			"items": []any{map[string]any{
				"item_id": item.ID, "source_project_id": project.ID,
			}},
		},
	)
	if err != nil {
		t.Fatalf("build Prompt session context: %v", err)
	}
	if promptApproval.ExecutionRule != projectcapabilities.RuleApprovalRequired ||
		promptApproval.CapabilityExecutionRule != projectcapabilities.RuleAlwaysRun ||
		promptApproval.ConnectorExecutionRule != string(connectortargets.ActionPermissionApprovalRequired) {
		t.Fatalf("connector Prompt must downgrade the effective session rule: %#v", promptApproval)
	}
	hidden := performJSON(
		fixture.server.Handler(),
		http.MethodPut,
		"/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/project-scopes",
		"",
		updateTokenProjectScopesRequest{EnabledProjectIDs: []int64{}},
	)
	if hidden.Code != http.StatusOK {
		t.Fatalf("hide Vault source project: %d %s", hidden.Code, hidden.Body.String())
	}
	if _, err := validateVaultApprovalAuthorization(ctx, fixture.server, fixture.server.activeRuntime(), vaultrequests.Request{
		TokenID: token.ID, ProjectID: project.ID, ActionName: vaultrequests.ActionRestartSession,
	}, promptApproval); !isVaultContextDrift(err) {
		t.Fatalf("project scope drift should stale the approval, got %v", err)
	}
}

func TestVaultActionCompensationRemovesGeneratedItemAndSession(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	project, err := projectstore.NewStore(fixture.db).Create(ctx, "Compensation Project")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	store, err := projectvault.NewStore(
		fixture.db,
		fixture.server.activeRuntime().vault,
		fixture.server.activeRuntime().workspaceUUID,
	)
	if err != nil {
		t.Fatalf("create Vault store: %v", err)
	}
	item, err := store.Create(ctx, projectvault.CreateInput{
		Name: "COMPENSATED_TOKEN", OwnerProjectID: project.ID,
		SecretType: "api_key", Source: "generated", GeneratorKind: "random_token",
	})
	if err != nil {
		t.Fatalf("create generated Vault item: %v", err)
	}
	if err := compensateVaultActionEffect(ctx, fixture.server.activeRuntime(), vaultrequests.Request{
		ActionName: vaultrequests.ActionGenerateItem,
	}, map[string]any{
		"item": map[string]any{
			"item_id": item.ID, "value_version": item.ValueVersion, "metadata_revision": item.MetadataRevision,
		},
	}); err != nil {
		t.Fatalf("compensate generated item: %v", err)
	}
	if _, err := store.Get(ctx, item.ID); err == nil {
		t.Fatalf("compensated generated item still exists")
	}

	target := fixture.createKeyAndServer(t, "compensated-session")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := fixture.db.ExecContext(ctx, `
		INSERT INTO console_sessions (runtime_id, generation, name, status, created_at, updated_at)
		VALUES (?, 1, 'compensated session', 'connected', ?, ?)`,
		target.ID, now, now,
	)
	if err != nil {
		t.Fatalf("insert compensated session: %v", err)
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read compensated session id: %v", err)
	}
	if err := compensateVaultActionEffect(ctx, fixture.server.activeRuntime(), vaultrequests.Request{
		ActionName: vaultrequests.ActionRestartSession,
	}, map[string]any{"session_id": sessionID}); err != nil {
		t.Fatalf("compensate Vault session: %v", err)
	}
	var status string
	if err := fixture.db.QueryRowContext(ctx, `SELECT status FROM console_sessions WHERE id = ?`, sessionID).Scan(&status); err != nil {
		t.Fatalf("read compensated session: %v", err)
	}
	if status != "closed" {
		t.Fatalf("compensated session status = %q", status)
	}
}
