package accesscontrol

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type authorizationFixture struct {
	database  *sql.DB
	tokenID   int64
	sessionID int64
	requestID int64
	actionID  int64
}

func newAuthorizationFixture(t *testing.T) authorizationFixture {
	t.Helper()
	ctx := t.Context()
	database := openTestDatabase(t)
	project, err := projects.NewStore(database).Create(ctx, "Authorization project")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(database).Create(ctx, tokens.CreateRequest{Name: "authorization agent"})
	if err != nil {
		t.Fatal(err)
	}
	targets := connectortargets.NewStore(database)
	target, err := targets.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ProjectID: project.ID, ConnectorKind: "test", Name: "Authorization target",
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := targets.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "test", Kind: "test", Label: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	surface, err := targets.EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: "test", TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: connectortargets.RuntimeCapabilityLiveConsole,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := database.ExecContext(ctx, `
		INSERT INTO console_sessions (runtime_id, generation, name, status, created_at, updated_at)
		VALUES (?, 1, 'authorization session', 'connected', ?, ?)`, surface.ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO vault_session_leases (
			token_id, runtime_id, session_id, session_generation, approval_context_hash,
			status, expires_at, created_at, updated_at
		) VALUES (?, ?, ?, 1, 'approval', 'active', ?, ?, ?)`,
		token.ID, surface.ID, sessionID, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), now, now,
	); err != nil {
		t.Fatal(err)
	}
	runtimeID := surface.ID
	request, _, err := vaultrequests.NewStore(database).Create(ctx, vaultrequests.CreateInput{
		TokenID: token.ID, ProjectID: project.ID, RuntimeID: &runtimeID,
		ActionName: vaultrequests.ActionRestartSession, Input: map[string]any{"target_ref": "test:1:1"},
		ApprovalContextHash: "approval", IdempotencyKey: "authorization-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	tokenID := token.ID
	actionRequest, err := targets.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID: &tokenID, TargetID: target.ID, ProfileID: profile.ID,
		ConnectorKind: "test", ActionName: "inspect", Source: "mcp",
		Status: connectors.ResultApprovalPending, EncryptedPayloadJSON: "sealed",
		ApprovalContext: `{"permission":"prompt"}`, ApprovalContextHash: "approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	return authorizationFixture{
		database: database, tokenID: token.ID, sessionID: sessionID,
		requestID: request.ID, actionID: actionRequest.ID,
	}
}

func (f authorizationFixture) scope(failAfterMutation error, finish func(context.Context, int64, []int64)) Scope {
	return Scope{
		Mutate:                  auditRunner(f.database, failAfterMutation),
		AcquireExclusive:        func(context.Context) (func(), error) { return func() {}, nil },
		FinishTokenInvalidation: finish,
	}
}

func TestAuthorizationMutationCommitsAuditAndPersistentInvalidationBeforeLiveCleanup(t *testing.T) {
	fixture := newAuthorizationFixture(t)
	finishCalled := false
	scope := fixture.scope(nil, func(_ context.Context, tokenID int64, sessionIDs []int64) {
		finishCalled = true
		if tokenID != fixture.tokenID || len(sessionIDs) != 1 || sessionIDs[0] != fixture.sessionID {
			t.Fatalf("invalidation callback: token=%d sessions=%v", tokenID, sessionIDs)
		}
		assertAuthorizationState(t, fixture, "revoked", vaultrequests.StatusStale, connectors.ResultStale, 1)
	})
	changed, err := mutateAuthorization(
		t.Context(), scope, fixture.tokenID, "authorization.updated",
		func() any { return map[string]any{"token_id": fixture.tokenID} },
		"authorization changed", func(tx *sql.Tx) (bool, error) {
			_, err := tx.ExecContext(t.Context(), `UPDATE api_tokens SET updated_at = ? WHERE id = ?`,
				time.Now().UTC().Format(time.RFC3339Nano), fixture.tokenID)
			return true, err
		},
	)
	if err != nil || !changed || !finishCalled {
		t.Fatalf("mutation: changed=%v finish=%v err=%v", changed, finishCalled, err)
	}
	assertAuthorizationState(t, fixture, "revoked", vaultrequests.StatusStale, connectors.ResultStale, 1)
}

func TestAuthorizationMutationRollbackPreservesAuthorizationState(t *testing.T) {
	fixture := newAuthorizationFixture(t)
	forced := errors.New("forced audit failure")
	finishCalled := false
	changed, err := mutateAuthorization(
		t.Context(), fixture.scope(forced, func(context.Context, int64, []int64) {
			finishCalled = true
		}), fixture.tokenID, "authorization.updated", func() any { return nil },
		"authorization changed", func(*sql.Tx) (bool, error) { return true, nil },
	)
	if !errors.Is(err, forced) || changed || finishCalled {
		t.Fatalf("rollback: changed=%v finish=%v err=%v", changed, finishCalled, err)
	}
	assertAuthorizationState(t, fixture, "active", vaultrequests.StatusApprovalPending, connectors.ResultApprovalPending, 0)
}

func TestUnchangedAuthorizationDoesNotAuditOrInvalidate(t *testing.T) {
	fixture := newAuthorizationFixture(t)
	finishCalled := false
	changed, err := mutateAuthorization(
		t.Context(), fixture.scope(nil, func(context.Context, int64, []int64) {
			finishCalled = true
		}), fixture.tokenID, "authorization.updated", func() any { return nil },
		"authorization changed", func(*sql.Tx) (bool, error) { return false, nil },
	)
	if err != nil || changed || finishCalled {
		t.Fatalf("unchanged mutation: changed=%v finish=%v err=%v", changed, finishCalled, err)
	}
	assertAuthorizationState(t, fixture, "active", vaultrequests.StatusApprovalPending, connectors.ResultApprovalPending, 0)
}

func TestAuthorizationMutationFailsClosedWhenExclusiveLeaseCannotBeAcquired(t *testing.T) {
	fixture := newAuthorizationFixture(t)
	scope := fixture.scope(nil, func(context.Context, int64, []int64) {})
	scope.AcquireExclusive = func(context.Context) (func(), error) { return nil, context.DeadlineExceeded }
	mutateCalled := false
	changed, err := mutateAuthorization(
		t.Context(), scope, fixture.tokenID, "authorization.updated", func() any { return nil },
		"authorization changed", func(*sql.Tx) (bool, error) {
			mutateCalled = true
			return true, nil
		},
	)
	if !errors.Is(err, ErrVaultDeliveryCanceled) || changed || mutateCalled {
		t.Fatalf("exclusive acquisition: changed=%v mutate=%v err=%v", changed, mutateCalled, err)
	}
	assertAuthorizationState(t, fixture, "active", vaultrequests.StatusApprovalPending, connectors.ResultApprovalPending, 0)
}

func assertAuthorizationState(
	t *testing.T,
	fixture authorizationFixture,
	leaseStatus string,
	requestStatus string,
	actionStatus connectors.ResultStatus,
	audits int,
) {
	t.Helper()
	var gotLease, gotRequest, gotAction, gotHistory string
	if err := fixture.database.QueryRow(`SELECT status FROM vault_session_leases WHERE session_id = ?`, fixture.sessionID).Scan(&gotLease); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.QueryRow(`SELECT status FROM vault_action_requests WHERE id = ?`, fixture.requestID).Scan(&gotRequest); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.QueryRow(`SELECT status FROM connector_action_requests WHERE id = ?`, fixture.actionID).Scan(&gotAction); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.QueryRow(`
		SELECT status FROM history_entries
		WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, fixture.actionID).Scan(&gotHistory); err != nil {
		t.Fatal(err)
	}
	wantHistory := string(actionStatus)
	if actionStatus == connectors.ResultApprovalPending {
		wantHistory = "pending_approval"
	}
	if gotLease != leaseStatus || gotRequest != requestStatus || gotAction != string(actionStatus) || gotHistory != wantHistory || countRows(t, fixture.database, "audit_logs") != audits {
		t.Fatalf("state: lease=%q request=%q action=%q history=%q audits=%d", gotLease, gotRequest, gotAction, gotHistory, countRows(t, fixture.database, "audit_logs"))
	}
}
