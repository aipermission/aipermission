package vaultrequests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

type runtimeTestMutation struct {
	database *sql.DB
	mu       sync.Mutex
	actions  []string
	observed []string
}

func (m *runtimeTestMutation) WithMutation(
	ctx context.Context,
	_ string,
	_ *int64,
	_ int64,
	action string,
	payload func() any,
	mutate func(*sql.Tx) error,
) error {
	tx, err := m.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := mutate(tx); err != nil {
		return err
	}
	if payload == nil || payload() == nil {
		return errors.New("audit payload is required")
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	m.mu.Lock()
	m.actions = append(m.actions, action)
	m.mu.Unlock()
	return nil
}

func (m *runtimeTestMutation) Observe(
	_ context.Context,
	_ string,
	_ *int64,
	_ int64,
	action string,
	payload any,
) {
	if payload == nil {
		panic("audit observation payload is required")
	}
	m.mu.Lock()
	m.observed = append(m.observed, action)
	m.mu.Unlock()
}

func (m *runtimeTestMutation) snapshot() ([]string, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.actions...), append([]string(nil), m.observed...)
}

type runtimeHarness struct {
	runtime       *Runtime
	store         *Store
	mutations     *runtimeTestMutation
	database      *sql.DB
	tokenID       int64
	otherTokenID  int64
	projectID     int64
	projectRef    string
	prepareCalls  int
	executeCalls  int
	compensations int
	authorized    bool
	allowRequest  bool
	mcpStarted    bool
	runAlways     bool
}

func newRuntimeHarness(t *testing.T) *runtimeHarness {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "vault-runtime.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	project, err := projectstore.NewStore(database).Create(t.Context(), "Vault Runtime")
	if err != nil {
		t.Fatal(err)
	}
	tokenStore := tokens.NewStore(database)
	token, err := tokenStore.Create(t.Context(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	otherToken, err := tokenStore.Create(t.Context(), tokens.CreateRequest{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	harness := &runtimeHarness{
		database: database, store: NewStore(database),
		mutations: &runtimeTestMutation{database: database},
		tokenID:   token.ID, otherTokenID: otherToken.ID,
		projectID: project.ID, projectRef: strconv.FormatInt(project.ID, 10),
		authorized: true, allowRequest: true, mcpStarted: true,
	}
	runtime, err := NewRuntime(RuntimeDependencies{
		Store: harness.store, Mutations: harness.mutations,
		Prepare: func(_ context.Context, tokenID int64, projectRef, actionName string, input map[string]any) (PreparedAction, error) {
			harness.prepareCalls++
			if tokenID != harness.tokenID || projectRef != harness.projectRef {
				return PreparedAction{}, ErrProjectNotFound
			}
			return PreparedAction{
				ProjectID: harness.projectID, Input: input,
				ApprovalContext: ApprovalContext{
					Schema: ApprovalContextSchema, ActionName: actionName,
					TokenID: tokenID, ProjectID: harness.projectID,
				},
				ApprovalContextHash: "approval-hash", RunImmediately: harness.runAlways,
			}, nil
		},
		AuthorizeOutput: func(context.Context, Request) bool { return harness.authorized },
		AllowRequest:    func(int64) bool { return harness.allowRequest },
		Execute: func(_ context.Context, request Request) (any, error) {
			harness.executeCalls++
			return map[string]any{"request_id": request.ID}, nil
		},
		ExecuteAtomic: func(context.Context, Request, string, string, string) (WorkflowResult, bool, error) {
			return WorkflowResult{}, false, nil
		},
		Compensate: func(context.Context, Request, any) error {
			harness.compensations++
			return nil
		},
		RepairProjection: func(context.Context, int64) error { return nil },
		RedactError:      func(_ context.Context, err error) string { return "redacted: " + err.Error() },
		IsStale:          func(error) bool { return false },
		MCPStarted:       func() bool { return harness.mcpStarted },
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.runtime = runtime
	return harness
}

func (h *runtimeHarness) call(t *testing.T, key string) RequestView {
	t.Helper()
	view, err := h.runtime.Call(t.Context(), CallInput{
		TokenID: h.tokenID, ProjectRef: h.projectRef,
		ActionName: ActionGenerateItem,
		Input: map[string]any{
			"name": "PROJECT_KEY", "generator_kind": "hex_32",
		},
		Reason: "create a deployment key", IdempotencyKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func TestRuntimeCallCreatesPromptRequestAndReplaysIdempotently(t *testing.T) {
	harness := newRuntimeHarness(t)
	first := harness.call(t, "prompt-request")
	if first.Request.Status != StatusApprovalPending || !first.OutputAuthorized {
		t.Fatalf("first request = %#v", first)
	}
	second := harness.call(t, "prompt-request")
	if second.Request.ID != first.Request.ID || harness.prepareCalls != 1 || harness.executeCalls != 0 {
		t.Fatalf("replay = %#v prepare=%d execute=%d", second, harness.prepareCalls, harness.executeCalls)
	}
	actions, observed := harness.mutations.snapshot()
	if fmt.Sprint(actions) != "[mcp.vault_action.request.created]" ||
		fmt.Sprint(observed) != "[mcp.vault_action.approval_pending]" {
		t.Fatalf("actions=%v observed=%v", actions, observed)
	}

	_, err := harness.runtime.Call(t.Context(), CallInput{
		TokenID: harness.tokenID, ProjectRef: harness.projectRef,
		ActionName: ActionGenerateItem,
		Input:      map[string]any{"name": "OTHER_KEY", "generator_kind": "hex_32"},
		Reason:     "create a deployment key", IdempotencyKey: "prompt-request",
	})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
}

func TestRuntimeCallAlwaysExecutesThroughAuditedWorkflow(t *testing.T) {
	harness := newRuntimeHarness(t)
	harness.runAlways = true
	view := harness.call(t, "always-request")
	if view.Request.Status != StatusCompleted || harness.executeCalls != 1 || harness.compensations != 0 {
		t.Fatalf("request=%#v execute=%d compensate=%d", view.Request, harness.executeCalls, harness.compensations)
	}
	actions, observed := harness.mutations.snapshot()
	if fmt.Sprint(actions) != "[mcp.vault_action.request.created mcp.vault_action.completed]" || len(observed) != 0 {
		t.Fatalf("actions=%v observed=%v", actions, observed)
	}
}

func TestRuntimeGetOwnedConcealsTokensAndStalesDriftedApproval(t *testing.T) {
	harness := newRuntimeHarness(t)
	created := harness.call(t, "owned-request")
	if _, err := harness.runtime.GetOwned(t.Context(), created.Request.ID, harness.otherTokenID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other token error = %v", err)
	}
	harness.authorized = false
	view, err := harness.runtime.GetOwned(t.Context(), created.Request.ID, harness.tokenID)
	if err != nil {
		t.Fatal(err)
	}
	if view.OutputAuthorized || view.Request.Status != StatusStale {
		t.Fatalf("drifted request = %#v", view)
	}
}

func TestRuntimeApprovalAndCancellationTransitions(t *testing.T) {
	t.Run("run", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		created := harness.call(t, "run-request")
		result, err := harness.runtime.RunPending(t.Context(), created.Request.ID, "approved")
		if err != nil || result.Request.Status != StatusCompleted || harness.executeCalls != 1 {
			t.Fatalf("result=%#v execute=%d err=%v", result, harness.executeCalls, err)
		}
	})
	t.Run("stopped", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		created := harness.call(t, "stopped-request")
		harness.mcpStarted = false
		if _, err := harness.runtime.RunPending(t.Context(), created.Request.ID, ""); !errors.Is(err, ErrMCPExecutionStopped) {
			t.Fatalf("RunPending() error = %v", err)
		}
	})
	t.Run("decline", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		created := harness.call(t, "decline-request")
		item, err := harness.runtime.DeclinePending(t.Context(), created.Request.ID, "not now")
		if err != nil || item.Status != StatusDeclined || item.UserNote != "not now" {
			t.Fatalf("item=%#v err=%v", item, err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		created := harness.call(t, "cancel-request")
		if _, err := harness.runtime.CancelOwned(t.Context(), created.Request.ID, harness.otherTokenID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("other token cancel error = %v", err)
		}
		item, err := harness.runtime.CancelOwned(t.Context(), created.Request.ID, harness.tokenID)
		if err != nil || item.Status != StatusCanceled {
			t.Fatalf("item=%#v err=%v", item, err)
		}
	})
}

func TestRuntimeValidationAndRateLimitDoNotPersistRequests(t *testing.T) {
	harness := newRuntimeHarness(t)
	_, err := harness.runtime.Call(t.Context(), CallInput{
		TokenID: harness.tokenID, ProjectRef: harness.projectRef,
		ActionName: ActionGenerateItem, Input: map[string]any{"name": "PROJECT_KEY", "unknown": true},
		Reason: "unknown field", IdempotencyKey: "invalid-request",
	})
	var validationError ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("validation error = %v", err)
	}
	harness.allowRequest = false
	_, err = harness.runtime.Call(t.Context(), CallInput{
		TokenID: harness.tokenID, ProjectRef: harness.projectRef,
		ActionName: ActionGenerateItem,
		Input:      map[string]any{"name": "PROJECT_KEY", "generator_kind": "hex_32"},
		Reason:     "limited request", IdempotencyKey: "limited-request",
	})
	if !errors.Is(err, ErrRequestRateLimited) {
		t.Fatalf("rate limit error = %v", err)
	}
	items, err := harness.store.List(t.Context(), "", 100)
	if err != nil || len(items) != 0 {
		t.Fatalf("persisted requests = %#v err=%v", items, err)
	}
}
