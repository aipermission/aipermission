package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/projectcapabilities"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func TestConnectorPeerTrustChangeInvalidatesVaultStateBeforeMutation(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	runtime := fixture.server.activeRuntime()
	project, err := projectstore.NewStore(fixture.db).Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	target := fixture.createKeyAndServer(t, "vault-invalidation")
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "vault-invalidation-token"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := fixture.db.ExecContext(ctx, `
		INSERT INTO console_sessions (
			runtime_id, generation, principal_kind, principal_token_id,
			workspace_id, runtime_instance_id, environment_content_hash,
			approval_context_hash, name, status, created_at, updated_at
		) VALUES (?, 1, 'mcp_token', ?, ?, ?, 'environment-hash',
		          'approval-hash', 'Vault invalidation session', 'connected', ?, ?)`,
		target.ID, token.ID, runtime.workspaceUUID, runtime.runtimeInstanceID, now, now,
	)
	if err != nil {
		t.Fatalf("insert Vault console session: %v", err)
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	lease := vaultsessions.Lease{
		WorkspaceID: runtime.workspaceUUID, RuntimeInstanceID: runtime.runtimeInstanceID,
		TokenID: token.ID, RuntimeID: target.ID, SessionID: sessionID,
		SessionGeneration: 1, EnvironmentContentHash: "environment-hash", ApprovalContextHash: "approval-hash",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := runtime.vaultLeases.Grant(lease); err != nil {
		t.Fatalf("grant Vault lease: %v", err)
	}
	if err := persistVaultLease(ctx, runtime, project.ID, lease); err != nil {
		t.Fatalf("persist Vault lease: %v", err)
	}
	pending, _, err := vaultrequests.NewStore(fixture.db).Create(ctx, vaultrequests.CreateInput{
		TokenID: token.ID, ProjectID: project.ID, RuntimeID: &target.ID,
		ActionName:          vaultrequests.ActionRestartSession,
		Input:               map[string]any{"target_ref": target.TargetRef},
		ApprovalContextHash: "pending-context", IdempotencyKey: "pending-runtime-request",
	})
	if err != nil {
		t.Fatalf("create pending Vault request: %v", err)
	}

	changeCalled := false
	if err := fixture.server.ConnectorChangeVaultPeerTrust(ctx, func() error {
		changeCalled = true
		var sessionStatus, leaseStatus string
		if err := fixture.db.QueryRowContext(ctx, `SELECT status FROM console_sessions WHERE id = ?`, sessionID).Scan(&sessionStatus); err != nil {
			return err
		}
		if err := fixture.db.QueryRowContext(ctx, `SELECT status FROM vault_session_leases WHERE session_id = ?`, sessionID).Scan(&leaseStatus); err != nil {
			return err
		}
		stale, err := vaultrequests.NewStore(fixture.db).Get(ctx, pending.ID)
		if err != nil {
			return err
		}
		if sessionStatus != "closed" || leaseStatus != "revoked" || stale.Status != vaultrequests.StatusStale {
			t.Fatalf("state before peer trust mutation: session=%q lease=%q request=%q", sessionStatus, leaseStatus, stale.Status)
		}
		return nil
	}); err != nil {
		t.Fatalf("change connector peer trust: %v", err)
	}
	if !changeCalled {
		t.Fatal("peer trust mutation callback was not called")
	}
}

func TestMCPStopStalesPendingVaultActions(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	project, err := projectstore.NewStore(fixture.db).Create(ctx, "MCP Stop Vault")
	if err != nil {
		t.Fatal(err)
	}
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "mcp-stop-vault-token"})
	if err != nil {
		t.Fatal(err)
	}
	capabilityPath := "/api/tokens/" + strconv.FormatInt(token.ID, 10) + "/project-capabilities"
	response := performJSON(fixture.server.Handler(), http.MethodPut, capabilityPath, "", updateProjectCapabilitiesRequest{
		Capabilities: []projectCapabilityInput{{
			ProjectID: project.ID, CapabilityName: projectcapabilities.VaultItemGenerate,
			ExecutionRule: projectcapabilities.RuleApprovalRequired,
		}},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("set Vault capability: %d %s", response.Code, response.Body.String())
	}
	pending, _, err := vaultrequests.NewStore(fixture.db).Create(ctx, vaultrequests.CreateInput{
		TokenID: token.ID, ProjectID: project.ID,
		ActionName:          vaultrequests.ActionGenerateItem,
		Input:               map[string]any{"name": "STOPPED_MCP_TOKEN"},
		ApprovalContextHash: "mcp-stop-context", IdempotencyKey: "mcp-stop-request",
	})
	if err != nil {
		t.Fatalf("create pending request: %v", err)
	}
	running, _, err := vaultrequests.NewStore(fixture.db).Create(ctx, vaultrequests.CreateInput{
		TokenID: token.ID, ProjectID: project.ID,
		ActionName:          vaultrequests.ActionGenerateItem,
		Input:               map[string]any{"name": "STOPPED_RUNNING_TOKEN"},
		ApprovalContextHash: "mcp-stop-running-context", IdempotencyKey: "mcp-stop-running-request",
	})
	if err != nil {
		t.Fatalf("create running request: %v", err)
	}
	if running, err = vaultrequests.NewStore(fixture.db).Claim(ctx, running.ID); err != nil {
		t.Fatalf("claim running request: %v", err)
	}

	stop := performJSON(
		fixture.server.Handler(),
		http.MethodPut,
		"/api/settings/mcp-runtime",
		"",
		updateMCPRuntimeRequest{Enabled: false},
	)
	if stop.Code != http.StatusOK {
		t.Fatalf("stop MCP: %d %s", stop.Code, stop.Body.String())
	}
	stale, err := vaultrequests.NewStore(fixture.db).Get(ctx, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Status != vaultrequests.StatusStale {
		t.Fatalf("pending Vault request after MCP stop = %q", stale.Status)
	}
	failed, err := vaultrequests.NewStore(fixture.db).Get(ctx, running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != vaultrequests.StatusFailed {
		t.Fatalf("running Vault request after MCP stop = %q", failed.Status)
	}
}

func TestIdenticalTokenAuthorizationUpdatesPreserveVaultSessionState(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := t.Context()
	runtime := fixture.server.activeRuntime()
	project, err := projectstore.NewStore(fixture.db).Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	target := fixture.createKeyAndServer(t, "vault-noop-authorization")
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "vault-noop-token"})
	if err != nil {
		t.Fatal(err)
	}
	tokenPath := "/api/tokens/" + strconv.FormatInt(token.ID, 10)
	scopeRequest := updateTokenProjectScopesRequest{EnabledProjectIDs: []int64{project.ID}}
	capabilityRequest := updateProjectCapabilitiesRequest{Capabilities: []projectCapabilityInput{{
		ProjectID: project.ID, CapabilityName: projectcapabilities.VaultSessionApply,
		ExecutionRule: projectcapabilities.RuleAlwaysRun,
	}}}
	permissionRequest := updateConnectorPermissionsRequest{Permissions: []connectorPermissionInput{{
		TargetID: target.TargetID, ProfileID: target.ProfileID,
		ActionName: sshconnector.ActionExec, ExecutionRule: string(connectortargets.ActionPermissionAlwaysRun),
	}}}
	for _, setup := range []struct {
		path          string
		body          any
		expectChanged bool
	}{
		{path: tokenPath + "/project-scopes", body: scopeRequest},
		{path: tokenPath + "/project-capabilities", body: capabilityRequest, expectChanged: true},
		{path: tokenPath + "/connector-permissions", body: permissionRequest, expectChanged: true},
	} {
		response := performJSON(fixture.server.Handler(), http.MethodPut, setup.path, "", setup.body)
		expected := `"changed":` + strconv.FormatBool(setup.expectChanged)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("initial authorization update %s: %d %s", setup.path, response.Code, response.Body.String())
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := fixture.db.ExecContext(ctx, `
		INSERT INTO console_sessions (
			runtime_id, generation, principal_kind, principal_token_id,
			workspace_id, runtime_instance_id, environment_content_hash,
			approval_context_hash, name, status, created_at, updated_at
		) VALUES (?, 1, 'mcp_token', ?, ?, ?, 'environment-hash',
		          'approval-hash', 'Vault no-op session', 'connected', ?, ?)`,
		target.ID, token.ID, runtime.workspaceUUID, runtime.runtimeInstanceID, now, now,
	)
	if err != nil {
		t.Fatalf("insert Vault console session: %v", err)
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	lease := vaultsessions.Lease{
		WorkspaceID: runtime.workspaceUUID, RuntimeInstanceID: runtime.runtimeInstanceID,
		TokenID: token.ID, RuntimeID: target.ID, SessionID: sessionID,
		SessionGeneration: 1, EnvironmentContentHash: "environment-hash", ApprovalContextHash: "approval-hash",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := runtime.vaultLeases.Grant(lease); err != nil {
		t.Fatalf("grant Vault lease: %v", err)
	}
	if err := persistVaultLease(ctx, runtime, project.ID, lease); err != nil {
		t.Fatalf("persist Vault lease: %v", err)
	}
	pending, _, err := vaultrequests.NewStore(fixture.db).Create(ctx, vaultrequests.CreateInput{
		TokenID: token.ID, ProjectID: project.ID, RuntimeID: &target.ID,
		ActionName:          vaultrequests.ActionRestartSession,
		Input:               map[string]any{"target_ref": target.TargetRef},
		ApprovalContextHash: "pending-context", IdempotencyKey: "pending-noop-request",
	})
	if err != nil {
		t.Fatalf("create pending Vault request: %v", err)
	}

	for _, update := range []struct {
		path string
		body any
	}{
		{path: tokenPath + "/project-scopes", body: scopeRequest},
		{path: tokenPath + "/project-capabilities", body: capabilityRequest},
		{path: tokenPath + "/connector-permissions", body: permissionRequest},
	} {
		response := performJSON(fixture.server.Handler(), http.MethodPut, update.path, "", update.body)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"changed":false`) {
			t.Fatalf("no-op authorization update %s: %d %s", update.path, response.Code, response.Body.String())
		}
	}

	var sessionStatus, leaseStatus string
	if err := fixture.db.QueryRowContext(ctx, `SELECT status FROM console_sessions WHERE id = ?`, sessionID).Scan(&sessionStatus); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRowContext(ctx, `SELECT status FROM vault_session_leases WHERE session_id = ?`, sessionID).Scan(&leaseStatus); err != nil {
		t.Fatal(err)
	}
	request, err := vaultrequests.NewStore(fixture.db).Get(ctx, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sessionStatus != "connected" || leaseStatus != "active" || request.Status != vaultrequests.StatusApprovalPending {
		t.Fatalf("no-op authorization update changed Vault state: session=%q lease=%q request=%q", sessionStatus, leaseStatus, request.Status)
	}
	principal, err := executionprincipal.MCPToken(token.ID, runtime.workspaceUUID, runtime.runtimeInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	authorization := console.SessionAuthorization{
		Handle:                 console.SessionHandle{ID: sessionID, RuntimeID: target.ID, Generation: 1},
		EnvironmentContentHash: "environment-hash", ApprovalContextHash: "approval-hash",
	}
	if err := runtime.vaultLeases.Authorize(ctx, principal, authorization, console.OperationObserve); err != nil {
		t.Fatalf("no-op authorization update revoked the in-memory Vault lease: %v", err)
	}
}
