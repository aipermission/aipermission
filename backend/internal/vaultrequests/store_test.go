package vaultrequests

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestStoreUsesTokenScopedIdempotencyAndStrictTransitions(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "requests.db"), "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	ctx := context.Background()
	project, err := projectstore.NewStore(database).Create(ctx, "Vault Requests")
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	token, err := tokens.NewStore(database).Create(ctx, tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	store := NewStore(database)
	input := CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionGenerateItem,
		Input: map[string]any{"name": "PROJECT_KEY"}, ApprovalContextHash: "context",
		IdempotencyKey: "same-request",
	}
	first, created, err := store.Create(ctx, input)
	if err != nil || !created {
		t.Fatalf("first create = %#v %v %v", first, created, err)
	}
	second, created, err := store.Create(ctx, input)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("idempotent create = %#v %v %v", second, created, err)
	}
	restartedInput := input
	restartedInput.ApprovalContextHash = "context-after-runtime-restart"
	restartedInput.ApprovalContext = map[string]any{"runtime_instance_id": "new-runtime"}
	restarted, created, err := store.Create(ctx, restartedInput)
	if err != nil || created || restarted.ID != first.ID {
		t.Fatalf("idempotent replay after approval-context drift = %#v %v %v", restarted, created, err)
	}
	conflicting := input
	conflicting.Input = map[string]any{"name": "OTHER_KEY"}
	if _, _, err := store.Create(ctx, conflicting); err != ErrIdempotencyConflict {
		t.Fatalf("conflicting idempotency key = %v", err)
	}
	if _, err := store.Claim(ctx, first.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := store.Claim(ctx, first.ID); err != ErrNotPending {
		t.Fatalf("second claim = %v", err)
	}
	completed, err := store.Complete(ctx, first.ID, StatusCompleted, map[string]any{"ok": true}, "", "approved")
	if err != nil || completed.Status != StatusCompleted {
		t.Fatalf("complete = %#v %v", completed, err)
	}

	running, created, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionGenerateItem,
		Input: map[string]any{"name": "ALWAYS_KEY"}, ApprovalContextHash: "always-context",
		IdempotencyKey: "always-request", InitialStatus: StatusRunning,
	})
	if err != nil || !created || running.Status != StatusRunning {
		t.Fatalf("running create = %#v %v %v", running, created, err)
	}
	if _, err := store.Claim(ctx, running.ID); err != ErrNotPending {
		t.Fatalf("running request must not be claimable: %v", err)
	}
	completedRunning, err := store.Complete(ctx, running.ID, StatusCompleted, map[string]any{"ok": true}, "", "")
	if err != nil || completedRunning.Status != StatusCompleted {
		t.Fatalf("complete running request = %#v err=%v", completedRunning, err)
	}

	pending, _, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionRestartSession,
		Input: map[string]any{"target_ref": "ssh:1:1"},
		ApprovalContext: map[string]any{"items": []any{map[string]any{
			"item_id": float64(42), "binding_id": float64(9),
		}}},
		ApprovalContextHash: "session-context", IdempotencyKey: "session-request",
	})
	if err != nil {
		t.Fatalf("create session request: %v", err)
	}
	if err := store.StalePendingForContext(ctx, 42, 0, "item changed"); err != nil {
		t.Fatalf("stale item requests: %v", err)
	}
	stale, err := store.Get(ctx, pending.ID)
	if err != nil || stale.Status != StatusStale {
		t.Fatalf("stale request = %#v %v", stale, err)
	}

	pendingByAction, _, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionRestartSession,
		Input:               map[string]any{"target_ref": "ssh:2:2"},
		ApprovalContextHash: "session-action-context", IdempotencyKey: "session-action-request",
	})
	if err != nil {
		t.Fatalf("create action-scoped request: %v", err)
	}
	unrelatedPending, _, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionGenerateItem,
		Input:               map[string]any{"name": "UNCHANGED_KEY"},
		ApprovalContextHash: "generate-action-context", IdempotencyKey: "generate-action-request",
	})
	if err != nil {
		t.Fatalf("create unrelated request: %v", err)
	}
	if err := store.StalePendingForAction(ctx, ActionRestartSession, "peer trust changed"); err != nil {
		t.Fatalf("stale requests by action: %v", err)
	}
	staleByAction, err := store.Get(ctx, pendingByAction.ID)
	if err != nil || staleByAction.Status != StatusStale {
		t.Fatalf("action-scoped stale request = %#v %v", staleByAction, err)
	}
	stillPending, err := store.Get(ctx, unrelatedPending.ID)
	if err != nil || stillPending.Status != StatusApprovalPending {
		t.Fatalf("unrelated request = %#v %v", stillPending, err)
	}

	target, err := connectortargets.NewStore(database).CreateTarget(ctx, connectortargets.CreateTargetInput{
		ProjectID: project.ID, ConnectorKind: "test", Name: "Runtime request target",
	})
	if err != nil {
		t.Fatalf("create runtime request target: %v", err)
	}
	profile, err := connectortargets.NewStore(database).CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "test", Kind: "test",
		Label: "Runtime request profile", EncryptedSecretJSON: "{}",
	})
	if err != nil {
		t.Fatalf("create runtime request profile: %v", err)
	}
	surface, err := connectortargets.NewStore(database).EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: "test", TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: "live_console", Label: "Runtime request surface",
	})
	if err != nil {
		t.Fatalf("create runtime request surface: %v", err)
	}
	runtimeID := surface.ID
	pendingByRuntime, _, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, RuntimeID: &runtimeID,
		ActionName: ActionRestartSession, Input: map[string]any{"target_ref": "ssh:3:3"},
		ApprovalContextHash: "runtime-context", IdempotencyKey: "runtime-request",
	})
	if err != nil {
		t.Fatalf("create runtime-scoped request: %v", err)
	}
	if err := store.StalePendingForRuntimes(ctx, []int64{runtimeID, runtimeID, 0}, "runtime changed"); err != nil {
		t.Fatalf("stale requests by runtime: %v", err)
	}
	staleByRuntime, err := store.Get(ctx, pendingByRuntime.ID)
	if err != nil || staleByRuntime.Status != StatusStale {
		t.Fatalf("runtime-scoped stale request = %#v %v", staleByRuntime, err)
	}

	cancelable, _, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionGenerateItem,
		Input: map[string]any{"name": "CANCELED_KEY"}, ApprovalContextHash: "cancel-context",
		IdempotencyKey: "cancel-request",
	})
	if err != nil {
		t.Fatalf("create cancelable request: %v", err)
	}
	canceled, err := store.CancelOwned(ctx, cancelable.ID, token.ID)
	if err != nil || canceled.Status != StatusCanceled {
		t.Fatalf("cancel request = %#v %v", canceled, err)
	}
	if _, err := store.CancelOwned(ctx, cancelable.ID, token.ID); err != ErrNotPending {
		t.Fatalf("cancel terminal request = %v", err)
	}

	expiring, _, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionGenerateItem,
		Input: map[string]any{"name": "EXPIRING_KEY"}, ApprovalContextHash: "expiry-context",
		IdempotencyKey: "expiry-request",
	})
	if err != nil {
		t.Fatalf("create expiring request: %v", err)
	}
	if _, err := database.Exec(`UPDATE vault_action_requests SET expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), expiring.ID); err != nil {
		t.Fatalf("expire request fixture: %v", err)
	}
	expired, err := store.Get(ctx, expiring.ID)
	if err != nil || expired.Status != StatusExpired {
		t.Fatalf("expired request = %#v %v", expired, err)
	}
	if _, err := store.Claim(ctx, expiring.ID); err != ErrNotPending {
		t.Fatalf("claim expired request = %v", err)
	}
	replayedExpired, created, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionGenerateItem,
		Input: map[string]any{"name": "EXPIRING_KEY"}, ApprovalContextHash: "expiry-context",
		IdempotencyKey: "expiry-request",
	})
	if err != nil || created || replayedExpired.Status != StatusExpired {
		t.Fatalf("replay expired request = %#v created=%v err=%v", replayedExpired, created, err)
	}

	runningRequest, _, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionGenerateItem,
		Input: map[string]any{"name": "RESTARTED_KEY"}, ApprovalContextHash: "restart-context",
		IdempotencyKey: "restart-request",
	})
	if err != nil {
		t.Fatalf("create restarted request: %v", err)
	}
	if _, err := store.Claim(ctx, runningRequest.ID); err != nil {
		t.Fatalf("claim restarted request: %v", err)
	}
	if err := store.FailRunning(ctx, "gateway restarted during execution"); err != nil {
		t.Fatalf("fail running requests: %v", err)
	}
	failed, err := store.Get(ctx, runningRequest.ID)
	if err != nil || failed.Status != StatusFailed {
		t.Fatalf("failed request = %#v %v", failed, err)
	}
	var historyStatus, historyError string
	if err := database.QueryRow(`
		SELECT status, error FROM history_entries
		WHERE source_ref_type = 'vault_action_request' AND source_ref_id = ?`,
		runningRequest.ID,
	).Scan(&historyStatus, &historyError); err != nil {
		t.Fatalf("read history projection: %v", err)
	}
	if historyStatus != StatusFailed || historyError != "gateway restarted during execution" {
		t.Fatalf("history projection status=%q error=%q", historyStatus, historyError)
	}

	declinable, _, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionGenerateItem,
		Input: map[string]any{"name": "DECLINED_KEY"}, ApprovalContextHash: "decline-context",
		IdempotencyKey: "decline-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	declined, err := store.Decline(ctx, declinable.ID, "not now")
	if err != nil || declined.Status != StatusDeclined || declined.UserNote != "not now" {
		t.Fatalf("decline = %#v err=%v", declined, err)
	}
	if _, err := store.Decline(ctx, declinable.ID, "again"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("decline terminal request = %v", err)
	}

	invalidTerminal, _, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionGenerateItem,
		Input: map[string]any{"name": "INVALID_TERMINAL_KEY"}, ApprovalContextHash: "invalid-terminal-context",
		IdempotencyKey: "invalid-terminal-request", InitialStatus: StatusRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(ctx, invalidTerminal.ID, StatusCanceled, nil, "", ""); err == nil {
		t.Fatal("invalid Complete status must fail")
	}
	if current, err := store.Get(ctx, invalidTerminal.ID); err != nil || current.Status != StatusRunning {
		t.Fatalf("invalid completion changed request = %#v err=%v", current, err)
	}

	singleStale, _, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionRestartSession,
		Input: map[string]any{"target_ref": "ssh:4:4"}, ApprovalContextHash: "single-stale-context",
		IdempotencyKey: "single-stale-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	staled, err := store.StalePending(ctx, singleStale.ID, "context changed")
	if err != nil || staled.Status != StatusStale || staled.Error != "context changed" {
		t.Fatalf("stale single request = %#v err=%v", staled, err)
	}
	replayedStale, created, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionRestartSession,
		Input: map[string]any{"target_ref": "ssh:4:4"}, ApprovalContextHash: "single-stale-context",
		IdempotencyKey: "single-stale-request",
	})
	if err != nil || created || replayedStale.Status != StatusStale {
		t.Fatalf("replay stale request = %#v created=%v err=%v", replayedStale, created, err)
	}
}

func TestClaimAllowsOnlyOneConcurrentApprover(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "claim-race.db"), "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	ctx := context.Background()
	project, err := projectstore.NewStore(database).Create(ctx, "Claim Race")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(database).Create(ctx, tokens.CreateRequest{Name: "claim-race"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	request, _, err := store.Create(ctx, CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: ActionGenerateItem,
		Input: map[string]any{"name": "CLAIM_RACE_KEY"}, ApprovalContextHash: "claim-race-context",
		IdempotencyKey: "claim-race-request",
	})
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for index := 0; index < 2; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, claimErr := store.Claim(context.Background(), request.ID)
			results <- claimErr
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	successes := 0
	notPending := 0
	for claimErr := range results {
		switch {
		case claimErr == nil:
			successes++
		case errors.Is(claimErr, ErrNotPending):
			notPending++
		default:
			t.Fatalf("claim error = %v", claimErr)
		}
	}
	if successes != 1 || notPending != 1 {
		t.Fatalf("concurrent claims: successes=%d not_pending=%d", successes, notPending)
	}
}
