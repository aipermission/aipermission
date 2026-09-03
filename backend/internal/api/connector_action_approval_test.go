package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	historypkg "github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestConnectorActionApprovalRoutesDeclinePendingRequest(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	result, err := fixture.server.callConnectorAction(context.Background(), fixture.server.activeRuntime(), connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}

	listResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-action-approvals?status=approval_pending", "", nil)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), strconv.FormatInt(result.Request.ID, 10)) {
		t.Fatalf("list connector approvals failed: %d %s", listResponse.Code, listResponse.Body.String())
	}
	detailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10), "", nil)
	if detailResponse.Code != http.StatusOK || !strings.Contains(detailResponse.Body.String(), "select 1") {
		t.Fatalf("approval detail must expose exact pending preview: %d %s", detailResponse.Code, detailResponse.Body.String())
	}
	var encryptedPayload string
	if err := fixture.db.QueryRow(`SELECT encrypted_payload_json FROM connector_action_requests WHERE id = ?`, result.Request.ID).Scan(&encryptedPayload); err != nil {
		t.Fatalf("read encrypted approval payload: %v", err)
	}
	if _, err := fixture.db.Exec(`UPDATE connector_action_requests SET encrypted_payload_json = 'invalid' WHERE id = ?`, result.Request.ID); err != nil {
		t.Fatalf("corrupt encrypted approval payload: %v", err)
	}
	redactedListResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-action-approvals?status=approval_pending", "", nil)
	if redactedListResponse.Code != http.StatusOK {
		t.Fatalf("approval list must not decrypt exact pending payloads: %d %s", redactedListResponse.Code, redactedListResponse.Body.String())
	}
	if _, err := fixture.db.Exec(`UPDATE connector_action_requests SET encrypted_payload_json = ? WHERE id = ?`, encryptedPayload, result.Request.ID); err != nil {
		t.Fatalf("restore encrypted approval payload: %v", err)
	}
	declineResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/decline", "", declineConnectorActionApprovalRequest{UserNote: "credential secret"})
	if declineResponse.Code != http.StatusOK || !strings.Contains(declineResponse.Body.String(), `"status":"declined"`) {
		t.Fatalf("decline connector approval failed: %d %s", declineResponse.Code, declineResponse.Body.String())
	}
	mcpResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-action-requests/"+strconv.FormatInt(result.Request.ID, 10), token.TokenValue, nil)
	if mcpResponse.Code != http.StatusOK || !strings.Contains(mcpResponse.Body.String(), `"status":"declined"`) || !strings.Contains(mcpResponse.Body.String(), "[REDACTED CREDENTIAL]") || strings.Contains(mcpResponse.Body.String(), "credential secret") {
		t.Fatalf("mcp connector request should show decline: %d %s", mcpResponse.Code, mcpResponse.Body.String())
	}
	var storedError string
	if err := fixture.db.QueryRow(`SELECT error FROM connector_action_requests WHERE id = ?`, result.Request.ID).Scan(&storedError); err != nil {
		t.Fatalf("read declined request error: %v", err)
	}
	if strings.Contains(storedError, "credential secret") || !strings.Contains(storedError, "[REDACTED CREDENTIAL]") {
		t.Fatalf("decline note was not credential-boundary redacted: %q", storedError)
	}
}

func TestConnectorActionApprovalRunUsesEncryptedInputNotRedactedDisplay(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	result, err := fixture.server.callConnectorAction(context.Background(), fixture.server.activeRuntime(), connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 'password=super-secret' as value"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}
	if !strings.Contains(fmt.Sprint(result.Request.Input["sql"]), "password=[REDACTED]") {
		t.Fatalf("display input should be redacted: %#v", result.Request.Input)
	}

	runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/run", "", runConnectorActionApprovalRequest{})
	if runResponse.Code != http.StatusOK {
		t.Fatalf("approval run should not fail stale because display input was redacted: %d %s", runResponse.Code, runResponse.Body.String())
	}
	finished, err := store.GetActionRequest(context.Background(), result.Request.ID)
	if err != nil {
		t.Fatalf("get finished connector request: %v", err)
	}
	if finished.Status == connectors.ResultStale {
		t.Fatalf("request should not become stale from redacted display input: %#v", finished)
	}
}

func TestConnectorActionApprovalRunDeliversUserNote(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	result, err := fixture.server.callConnectorAction(context.Background(), fixture.server.activeRuntime(), connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}

	runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/run", "", runConnectorActionApprovalRequest{UserNote: "only inspect metadata"})
	if runResponse.Code != http.StatusOK {
		t.Fatalf("approval run failed: %d %s", runResponse.Code, runResponse.Body.String())
	}
	var queued string
	if err := fixture.db.QueryRow(`
		SELECT message
		FROM message_queue
		WHERE token_id = ? AND direction = 'user_to_ai'
		ORDER BY id DESC
		LIMIT 1`,
		token.ID,
	).Scan(&queued); err != nil {
		t.Fatalf("read queued approval note: %v", err)
	}
	if !strings.Contains(queued, "only inspect metadata") {
		t.Fatalf("queued note = %q", queued)
	}
}

func TestConnectorActionApprovalRunMarksDriftStale(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	result, err := fixture.server.callConnectorAction(context.Background(), fixture.server.activeRuntime(), connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionBlocked,
	}); err != nil {
		t.Fatalf("block connector permission: %v", err)
	}

	runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/run", "", runConnectorActionApprovalRequest{})
	if runResponse.Code != http.StatusConflict || !strings.Contains(runResponse.Body.String(), "fresh request") {
		t.Fatalf("expected stale conflict, got %d %s", runResponse.Code, runResponse.Body.String())
	}
	stale, err := store.GetActionRequest(context.Background(), result.Request.ID)
	if err != nil {
		t.Fatalf("get stale connector request: %v", err)
	}
	if stale.Status != connectors.ResultStale {
		t.Fatalf("status = %q", stale.Status)
	}
	if stale.ApprovalContextDrift != "permission" {
		t.Fatalf("approval drift = %q", stale.ApprovalContextDrift)
	}
}

func TestConnectorActionApprovalRunMarksPrepareFailureStale(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID)
	badAction := "missing_action"
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    badAction,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	request, err := store.InsertActionRequest(context.Background(), connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        postgresconnector.Kind,
		ActionName:           badAction,
		Source:               commandRequestSourceMCP,
		Input:                map[string]any{},
		EncryptedPayloadJSON: "",
		Status:               connectors.ResultApprovalPending,
		ApprovalContextHash:  "old-context",
	})
	if err != nil {
		t.Fatalf("insert pending connector request: %v", err)
	}
	encryptedPayload, err := recordcrypto.EncryptJSON(fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID, recordcrypto.ConnectorActionRequest, request.ID, connectorActionExecutionEnvelope{
		Input:   map[string]any{},
		Payload: map[string]any{},
	})
	if err != nil {
		t.Fatalf("encrypt pending payload: %v", err)
	}
	if err := store.SetActionRequestEncryptedPayload(context.Background(), request.ID, encryptedPayload); err != nil {
		t.Fatalf("store pending payload: %v", err)
	}
	if err := historypkg.NewStore(fixture.db).SyncConnectorActionRequest(context.Background(), request.ID); err != nil {
		t.Fatalf("sync pending connector request: %v", err)
	}

	runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(request.ID, 10)+"/run", "", runConnectorActionApprovalRequest{})
	if runResponse.Code != http.StatusConflict || !strings.Contains(runResponse.Body.String(), "fresh request") {
		t.Fatalf("expected prepare drift conflict, got %d %s", runResponse.Code, runResponse.Body.String())
	}
	stale, err := store.GetActionRequest(context.Background(), request.ID)
	if err != nil {
		t.Fatalf("get stale connector request: %v", err)
	}
	if stale.Status != connectors.ResultStale || !strings.Contains(stale.Error, "fresh request") {
		t.Fatalf("request should be stale with fresh-request error: %#v", stale)
	}
	if stale.ApprovalContextDrift != "target_or_action" {
		t.Fatalf("approval drift = %q", stale.ApprovalContextDrift)
	}
	var historyStatus string
	if err := fixture.db.QueryRow(`
		SELECT status
		FROM history_entries
		WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`,
		request.ID,
	).Scan(&historyStatus); err != nil {
		t.Fatalf("read synced history: %v", err)
	}
	if historyStatus != string(connectors.ResultStale) {
		t.Fatalf("history status = %q", historyStatus)
	}
}

type approvalRetryPolicyConnector struct {
	localActionTestConnector
	retryPolicy connectors.RetryPolicy
}

func (connector *approvalRetryPolicyConnector) GetActionList(ctx context.Context, target connectors.TargetView, profile connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	actions, err := connector.localActionTestConnector.GetActionList(ctx, target, profile)
	if err != nil {
		return nil, err
	}
	actions[0].RetryPolicy = connector.retryPolicy
	return actions, nil
}

func TestConnectorActionApprovalRunRejectsRetryPolicyDrift(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "retry-policy-drift"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createApprovalObserverTargetProfile(t, store)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID: token.ID, TargetID: target.ID, ProfileID: profile.ID, ActionName: "echo",
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set permission: %v", err)
	}
	connector := &approvalRetryPolicyConnector{retryPolicy: connectors.RetryPolicy{Class: connectors.RetryReadOnly}}
	if err := fixture.server.activeRuntime().connectorRegistry().Register(connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	pending, err := fixture.server.callConnectorAction(ctx, fixture.server.activeRuntime(), connectorActionCall{
		Source: commandRequestSourceMCP, TokenID: token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo", Input: map[string]any{"value": "retry policy"}, Reason: "verify retry policy drift",
	})
	if err != nil {
		t.Fatalf("create pending request: %v", err)
	}
	connector.retryPolicy = connectors.RetryPolicy{Class: connectors.RetryIdempotent}

	runResponse := performJSON(
		fixture.server.Handler(), http.MethodPost,
		"/api/connector-action-approvals/"+strconv.FormatInt(pending.Request.ID, 10)+"/run", "",
		runConnectorActionApprovalRequest{},
	)
	if runResponse.Code != http.StatusConflict || !strings.Contains(runResponse.Body.String(), "fresh request") {
		t.Fatalf("retry policy drift response: %d %s", runResponse.Code, runResponse.Body.String())
	}
	stale, err := store.GetActionRequest(ctx, pending.Request.ID)
	if err != nil {
		t.Fatalf("read stale request: %v", err)
	}
	if stale.Status != connectors.ResultStale || stale.ApprovalContextDrift != "action_definition" {
		t.Fatalf("stale request = %#v", stale)
	}
}

func TestConnectorTargetAndProfileUpdatesStalePendingApprovals(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID)
	pendingTarget, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        postgresconnector.Kind,
		ActionName:           postgresconnector.ActionQueryReadonly,
		Input:                map[string]any{"sql": "select 1"},
		EncryptedPayloadJSON: "encrypted-payload",
		Status:               connectors.ResultApprovalPending,
	})
	if err != nil {
		t.Fatalf("insert pending target request: %v", err)
	}
	running, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        postgresconnector.Kind,
		ActionName:           postgresconnector.ActionQueryReadonly,
		Input:                map[string]any{"sql": "select pg_sleep(10)"},
		EncryptedPayloadJSON: "encrypted-payload",
		Status:               connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert running request: %v", err)
	}
	updateTarget := performJSON(fixture.server.Handler(), http.MethodPut, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10), "", updateConnectorTargetRequest{
		Name: "main-db-renamed",
		Config: map[string]any{
			"connection_mode": "direct",
			"host":            "127.0.0.1",
			"port":            5432,
			"database":        "app",
			"ssl_mode":        "prefer",
		},
	})
	if updateTarget.Code != http.StatusOK {
		t.Fatalf("update connector target failed: %d %s", updateTarget.Code, updateTarget.Body.String())
	}
	gotPendingTarget, err := store.GetActionRequest(ctx, pendingTarget.ID)
	if err != nil {
		t.Fatalf("get pending target request: %v", err)
	}
	if gotPendingTarget.Status != connectors.ResultStale || gotPendingTarget.ApprovalContextDrift != "target" {
		t.Fatalf("pending target request should be stale after target update: %#v", gotPendingTarget)
	}
	gotRunning, err := store.GetActionRequest(ctx, running.ID)
	if err != nil {
		t.Fatalf("get running request: %v", err)
	}
	if gotRunning.Status != connectors.ResultRunning {
		t.Fatalf("running request should not be stale from target update: %#v", gotRunning)
	}

	pendingProfile, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        postgresconnector.Kind,
		ActionName:           postgresconnector.ActionGetSchemas,
		Input:                map[string]any{},
		EncryptedPayloadJSON: "encrypted-payload",
		Status:               connectors.ResultApprovalPending,
	})
	if err != nil {
		t.Fatalf("insert pending profile request: %v", err)
	}
	updateProfile := performJSON(fixture.server.Handler(), http.MethodPut, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/profiles/"+strconv.FormatInt(profile.ID, 10), "", updateConnectorCredentialProfileRequest{
		Kind:  "username_password",
		Label: "readonly-renamed",
		Public: map[string]any{
			"username": "app_readonly",
		},
		RiskLabel: "read-only",
	})
	if updateProfile.Code != http.StatusOK {
		t.Fatalf("update connector profile failed: %d %s", updateProfile.Code, updateProfile.Body.String())
	}
	gotPendingProfile, err := store.GetActionRequest(ctx, pendingProfile.ID)
	if err != nil {
		t.Fatalf("get pending profile request: %v", err)
	}
	if gotPendingProfile.Status != connectors.ResultStale || gotPendingProfile.ApprovalContextDrift != "profile" {
		t.Fatalf("pending profile request should be stale after profile update: %#v", gotPendingProfile)
	}
}

func TestConnectorActionApprovalRunRequiresCurrentToken(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, apiTestFixture, tokens.CreateResponse){
		"revoked": func(t *testing.T, fixture apiTestFixture, token tokens.CreateResponse) {
			t.Helper()
			if _, err := fixture.tokens.Revoke(context.Background(), token.ID); err != nil {
				t.Fatalf("revoke token: %v", err)
			}
		},
		"expired": func(t *testing.T, fixture apiTestFixture, token tokens.CreateResponse) {
			t.Helper()
			if _, err := fixture.db.Exec(`UPDATE api_tokens SET expires_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), token.ID); err != nil {
				t.Fatalf("expire token: %v", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newAPITestFixture(t)
			store := connectortargets.NewStore(fixture.db)
			token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
			if err != nil {
				t.Fatalf("create token: %v", err)
			}
			target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID)
			if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
				TokenID:       token.ID,
				TargetID:      target.ID,
				ProfileID:     profile.ID,
				ActionName:    postgresconnector.ActionQueryReadonly,
				ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
			}); err != nil {
				t.Fatalf("set connector permission: %v", err)
			}
			result, err := fixture.server.callConnectorAction(context.Background(), fixture.server.activeRuntime(), connectorActionCall{
				Source:     commandRequestSourceMCP,
				TokenID:    token.ID,
				TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
				ActionName: postgresconnector.ActionQueryReadonly,
				Input:      map[string]any{"sql": "select 1"},
				Reason:     "smoke",
			})
			if err != nil {
				t.Fatalf("call connector action: %v", err)
			}

			mutate(t, fixture, token)
			runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/run", "", runConnectorActionApprovalRequest{})
			if runResponse.Code != http.StatusConflict || !strings.Contains(runResponse.Body.String(), "fresh request") {
				t.Fatalf("expected stale conflict, got %d %s", runResponse.Code, runResponse.Body.String())
			}
			stale, err := store.GetActionRequest(context.Background(), result.Request.ID)
			if err != nil {
				t.Fatalf("get stale connector request: %v", err)
			}
			if stale.Status != connectors.ResultStale {
				t.Fatalf("status = %q", stale.Status)
			}
		})
	}
}

type approvalExecutionObserverConnector struct {
	localActionTestConnector
	beforeExecute func(context.Context) error
	result        connectors.ActionResult
	executeError  error
}

func (c approvalExecutionObserverConnector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	if c.beforeExecute != nil {
		if err := c.beforeExecute(ctx); err != nil {
			return connectors.ActionResult{}, err
		}
	}
	if c.executeError != nil {
		return connectors.ActionResult{}, c.executeError
	}
	if c.result.Status != "" || c.result.Output != nil || c.result.DisplayText != "" || c.result.Error != "" {
		return c.result, nil
	}
	return c.localActionTestConnector.ExecuteAction(ctx, runtime, action)
}

func TestConnectorActionApprovalRunFinalizesExecutionFailure(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createApprovalObserverTargetProfile(t, store)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    "echo",
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}

	var requestID int64
	observer := approvalExecutionObserverConnector{
		beforeExecute: func(ctx context.Context) error {
			request, err := store.GetActionRequest(ctx, requestID)
			if err != nil {
				return fmt.Errorf("read request during execution: %w", err)
			}
			if request.Status != connectors.ResultRunning {
				return fmt.Errorf("request status during execution = %q", request.Status)
			}
			return nil
		},
		executeError: connectors.ClassifyError("fixture_failure", fmt.Errorf("fixture execution failed")),
	}
	if err := fixture.server.activeRuntime().connectorRegistry().Register(observer); err != nil {
		t.Fatalf("register observer connector: %v", err)
	}
	pending, err := fixture.server.callConnectorAction(ctx, fixture.server.activeRuntime(), connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo",
		Input:      map[string]any{"value": "fail"},
		Reason:     "approval failure transition test",
	})
	if err != nil {
		t.Fatalf("create pending connector action: %v", err)
	}
	requestID = pending.Request.ID

	runResponse := performJSON(
		fixture.server.Handler(), http.MethodPost,
		"/api/connector-action-approvals/"+strconv.FormatInt(requestID, 10)+"/run", "",
		runConnectorActionApprovalRequest{},
	)
	if runResponse.Code != http.StatusOK {
		t.Fatalf("run connector approval: %d %s", runResponse.Code, runResponse.Body.String())
	}
	failed, err := store.GetActionRequest(ctx, requestID)
	if err != nil {
		t.Fatalf("get failed connector request: %v", err)
	}
	if failed.Status != connectors.ResultFailed || !strings.Contains(failed.Error, "fixture execution failed") {
		t.Fatalf("unexpected failed request: %#v", failed)
	}
	var historyStatus string
	if err := fixture.db.QueryRowContext(ctx, `
		SELECT status FROM history_entries
		WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, requestID).Scan(&historyStatus); err != nil {
		t.Fatalf("read failed history entry: %v", err)
	}
	if historyStatus != string(connectors.ResultFailed) {
		t.Fatalf("history status = %q", historyStatus)
	}
}

func TestConnectorActionApprovalRunTransitionsBeforeExecutionAndCompletesAudit(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createApprovalObserverTargetProfile(t, store)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    "echo",
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}

	var requestID int64
	observer := approvalExecutionObserverConnector{
		beforeExecute: func(ctx context.Context) error {
			if _, active := fixture.server.activeRuntime().connectorCredentialBoundary(requestID); !active {
				return errors.New("approved request has no active execution boundary")
			}
			fixture.server.recoverOrphanedConnectorActions(ctx, fixture.server.activeRuntime(), time.Now().UTC())
			var requestStatus, historyStatus, note string
			if err := fixture.db.QueryRowContext(ctx, `SELECT status FROM connector_action_requests WHERE id = ?`, requestID).Scan(&requestStatus); err != nil {
				return fmt.Errorf("read request status during execution: %w", err)
			}
			if err := fixture.db.QueryRowContext(ctx, `
				SELECT status FROM history_entries
				WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, requestID).Scan(&historyStatus); err != nil {
				return fmt.Errorf("read history status during execution: %w", err)
			}
			if err := fixture.db.QueryRowContext(ctx, `
				SELECT message FROM message_queue
				WHERE token_id = ? AND direction = 'user_to_ai'
				ORDER BY id DESC LIMIT 1`, token.ID).Scan(&note); err != nil {
				return fmt.Errorf("read operator note during execution: %w", err)
			}
			if requestStatus != string(connectors.ResultRunning) || historyStatus != string(connectors.ResultRunning) {
				return fmt.Errorf("execution started before running state was persisted: request=%s history=%s", requestStatus, historyStatus)
			}
			if !strings.Contains(note, "inspect metadata only") {
				return fmt.Errorf("operator note was not delivered before execution: %q", note)
			}
			return nil
		},
		result: connectors.ActionResult{
			Status:      connectors.ResultCompleted,
			Output:      map[string]any{"echo": "approved"},
			DisplayText: "approved",
		},
	}
	if err := fixture.server.activeRuntime().connectorRegistry().Register(observer); err != nil {
		t.Fatalf("register observer connector: %v", err)
	}

	pending, err := fixture.server.callConnectorAction(ctx, fixture.server.activeRuntime(), connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo",
		Input:      map[string]any{"value": "approved"},
		Reason:     "approval transition test",
	})
	if err != nil {
		t.Fatalf("create pending connector action: %v", err)
	}
	requestID = pending.Request.ID
	if _, err := fixture.db.Exec(`UPDATE connector_action_requests SET created_at = datetime('now', '-2 minutes') WHERE id = ?`, requestID); err != nil {
		t.Fatalf("age pending connector action: %v", err)
	}

	runResponse := performJSON(
		fixture.server.Handler(), http.MethodPost,
		"/api/connector-action-approvals/"+strconv.FormatInt(requestID, 10)+"/run", "",
		runConnectorActionApprovalRequest{UserNote: "inspect metadata only"},
	)
	if runResponse.Code != http.StatusOK {
		t.Fatalf("run connector approval: %d %s", runResponse.Code, runResponse.Body.String())
	}
	finished, err := store.GetActionRequest(ctx, requestID)
	if err != nil {
		t.Fatalf("get completed connector request: %v", err)
	}
	output, ok := finished.Output.(map[string]any)
	if finished.Status != connectors.ResultCompleted || !ok || output["echo"] != "approved" {
		t.Fatalf("unexpected completed request: %#v", finished)
	}
	assertConnectorApprovalAuditOrder(t, fixture.db, requestID,
		"connector_action.request.created",
		"connector_action.request.running",
		"connector_action.request.completed",
		"connector_action.run.completed",
	)
}

func createApprovalObserverTargetProfile(t *testing.T, store *connectortargets.Store) (connectortargets.Target, connectortargets.CredentialProfile) {
	t.Helper()
	ctx := context.Background()
	target, err := store.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: localActionTestConnectorKind,
		Name:          "approval-observer",
		Config:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("create observer target: %v", err)
	}
	profile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID:      target.ID,
		ConnectorKind: localActionTestConnectorKind,
		Kind:          "default",
		Label:         "main",
		Public:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("create observer profile: %v", err)
	}
	return target, profile
}

func assertConnectorApprovalAuditOrder(t *testing.T, database *sql.DB, requestID int64, expected ...string) {
	t.Helper()
	rows, err := database.QueryContext(context.Background(), `
		SELECT action FROM audit_logs
		WHERE action_request_id = ?
		ORDER BY id`, requestID)
	if err != nil {
		t.Fatalf("list connector approval audit events: %v", err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatalf("scan connector approval audit event: %v", err)
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate connector approval audit events: %v", err)
	}
	if fmt.Sprint(actions) != fmt.Sprint(expected) {
		t.Fatalf("audit order = %v, want %v", actions, expected)
	}
}
