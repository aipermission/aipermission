package vaultrequests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

type runtimeTestMutation struct {
	database            *sql.DB
	delivery            *runtimeTestDelivery
	mu                  sync.Mutex
	actions             []string
	observed            []string
	createdWithDelivery bool
}

type runtimeTestDelivery struct {
	mu     sync.Mutex
	active int
}

func (d *runtimeTestDelivery) acquire(context.Context) (func(), error) {
	d.mu.Lock()
	d.active++
	d.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			d.mu.Lock()
			d.active--
			d.mu.Unlock()
		})
	}, nil
}

func (d *runtimeTestDelivery) held() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.active > 0
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
	if action == "mcp.vault_action.request.created" {
		m.createdWithDelivery = m.delivery != nil && m.delivery.held()
	}
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
	runtime              *Runtime
	store                *Store
	mutations            *runtimeTestMutation
	delivery             *runtimeTestDelivery
	database             *sql.DB
	tokenID              int64
	otherTokenID         int64
	projectID            int64
	projectRef           string
	prepareCalls         int
	executeCalls         int
	executedWithDelivery bool
	compensations        int
	authorization        OutputAuthorization
	openErr              error
	allowRequest         bool
	mcpStarted           bool
	runAlways            bool
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
	delivery := &runtimeTestDelivery{}
	harness := &runtimeHarness{
		database: database, store: NewStore(database),
		mutations: &runtimeTestMutation{database: database, delivery: delivery}, delivery: delivery,
		tokenID: token.ID, otherTokenID: otherToken.ID,
		projectID: project.ID, projectRef: strconv.FormatInt(project.ID, 10),
		authorization: OutputAuthorized, allowRequest: true, mcpStarted: true,
	}
	runtime, err := NewRuntime(RuntimeDependencies{
		Store: harness.store, Mutations: harness.mutations,
		ResolveProject: func(ctx context.Context, ref string) (int64, error) {
			project, resolveErr := projectstore.NewStore(database).ResolveRef(ctx, ref)
			if errors.Is(resolveErr, projectstore.ErrNotFound) {
				return 0, ErrProjectNotFound
			}
			if resolveErr != nil {
				return 0, resolveErr
			}
			return project.ID, nil
		},
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
		AuthorizeOutput: func(context.Context, Request) OutputAuthorization {
			if !harness.mcpStarted {
				return OutputWithheld
			}
			return harness.authorization
		},
		AllowRequest: func(int64) bool { return harness.allowRequest },
		Execute: func(_ context.Context, request Request) (any, error) {
			harness.executeCalls++
			harness.executedWithDelivery = delivery.held()
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
		RedactProjection: func(_ context.Context, value any) (any, error) { return value, nil },
		SealRequest: func(_ int64, envelope ExecutionEnvelope) (string, error) {
			encoded, err := json.Marshal(envelope)
			if err != nil {
				return "", err
			}
			sealed, err := json.Marshal(map[string]any{
				"version": 1, "algorithm": "AES-256-GCM", "nonce": "test", "ciphertext": string(encoded),
			})
			return string(sealed), err
		},
		OpenRequest: func(_ int64, sealed string) (ExecutionEnvelope, error) {
			if harness.openErr != nil {
				return ExecutionEnvelope{}, harness.openErr
			}
			var wrapper struct {
				Ciphertext string `json:"ciphertext"`
			}
			var envelope ExecutionEnvelope
			if err := json.Unmarshal([]byte(sealed), &wrapper); err != nil {
				return envelope, err
			}
			err := json.Unmarshal([]byte(wrapper.Ciphertext), &envelope)
			return envelope, err
		},
		IsStale:         func(error) bool { return false },
		MCPStarted:      func() bool { return harness.mcpStarted },
		AcquireDelivery: delivery.acquire,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.runtime = runtime
	return harness
}

func (h *runtimeHarness) call(t *testing.T, key string) Request {
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
	if first.Status != StatusApprovalPending {
		t.Fatalf("first request = %#v", first)
	}
	if !harness.mutations.createdWithDelivery || harness.delivery.held() {
		t.Fatalf("request insertion delivery lease: held_at_insert=%t held_after_return=%t", harness.mutations.createdWithDelivery, harness.delivery.held())
	}
	var delivered RequestView
	if err := harness.runtime.DeliverOwned(t.Context(), first.ID, harness.tokenID, func(view RequestView) {
		if !harness.delivery.held() {
			t.Fatal("owned output was delivered after releasing admission")
		}
		delivered = view
	}); err != nil || !delivered.OutputAuthorized || harness.delivery.held() {
		t.Fatalf("delivered view=%#v held=%v err=%v", delivered, harness.delivery.held(), err)
	}
	second := harness.call(t, "prompt-request")
	if second.ID != first.ID || harness.prepareCalls != 1 || harness.executeCalls != 0 {
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

func TestSameActionCallUsesResolvedProjectIdentity(t *testing.T) {
	request := Request{
		ProjectID: 42, ProjectSlug: "42", ActionName: ActionGenerateItem,
		Input: map[string]any{"name": "PROJECT_KEY"}, Reason: "generate key",
	}
	if !SameActionCall(request, 42, request.ActionName, request.Input, request.Reason) {
		t.Fatal("resolved project identity did not replay")
	}
	if SameActionCall(request, 43, request.ActionName, request.Input, request.Reason) {
		t.Fatal("different explicit project id replayed")
	}
}

func TestRuntimeCallCanonicalizesProjectReferenceBeforeReplay(t *testing.T) {
	harness := newRuntimeHarness(t)
	first := harness.call(t, "canonical-project-replay")
	for _, ref := range []string{"id:" + strconv.FormatInt(harness.projectID, 10), "id:+" + strconv.FormatInt(harness.projectID, 10)} {
		item, err := harness.runtime.Call(t.Context(), CallInput{
			TokenID: harness.tokenID, ProjectRef: ref, ActionName: ActionGenerateItem,
			Input:  map[string]any{"name": "PROJECT_KEY", "generator_kind": "hex_32"},
			Reason: "create a deployment key", IdempotencyKey: "canonical-project-replay",
		})
		if err != nil || item.ID != first.ID {
			t.Fatalf("replay through %q = %#v, err=%v", ref, item, err)
		}
	}
}

func TestRuntimeCallRejectsAmbiguousBareNumericProjectBeforeReplay(t *testing.T) {
	harness := newRuntimeHarness(t)
	_ = harness.call(t, "ambiguous-project-replay")
	conflicting, err := projectstore.NewStore(harness.database).Create(t.Context(), "Numeric Slug Collision")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.database.Exec(`UPDATE projects SET slug = ? WHERE id = ?`, harness.projectRef, conflicting.ID); err != nil {
		t.Fatal(err)
	}
	_, err = harness.runtime.Call(t.Context(), CallInput{
		TokenID: harness.tokenID, ProjectRef: harness.projectRef, ActionName: ActionGenerateItem,
		Input:  map[string]any{"name": "PROJECT_KEY", "generator_kind": "hex_32"},
		Reason: "create a deployment key", IdempotencyKey: "ambiguous-project-replay",
	})
	if !errors.Is(err, projectstore.ErrAmbiguousRef) {
		t.Fatalf("ambiguous replay error = %v", err)
	}
}

func TestRuntimeCallReplaysAfterProjectIsArchived(t *testing.T) {
	harness := newRuntimeHarness(t)
	first := harness.call(t, "archived-project-replay")
	if err := projectstore.NewStore(harness.database).Archive(t.Context(), harness.projectID); err != nil {
		t.Fatal(err)
	}
	second := harness.call(t, "archived-project-replay")
	if second.ID != first.ID || harness.prepareCalls != 1 {
		t.Fatalf("archived replay = %#v prepare=%d", second, harness.prepareCalls)
	}
}

func TestStoredProjectReferenceMatchesCanonicalIdentity(t *testing.T) {
	request := Request{ProjectID: 42, ProjectSlug: "project-42"}
	for _, ref := range []string{"42", "+42", "id:42", "id:+42", "project-42", "slug:project-42"} {
		if !storedProjectReferenceMatches(request, ref) {
			t.Fatalf("reference %q did not match", ref)
		}
	}
	for _, ref := range []string{"", "41", "id:41", "slug:other", "other"} {
		if storedProjectReferenceMatches(request, ref) {
			t.Fatalf("reference %q unexpectedly matched", ref)
		}
	}
}

func TestRuntimeCallAlwaysExecutesThroughAuditedWorkflow(t *testing.T) {
	harness := newRuntimeHarness(t)
	harness.runAlways = true
	request := harness.call(t, "always-request")
	if request.Status != StatusCompleted || harness.executeCalls != 1 || harness.compensations != 0 {
		t.Fatalf("request=%#v execute=%d compensate=%d", request, harness.executeCalls, harness.compensations)
	}
	if !harness.mutations.createdWithDelivery || harness.executedWithDelivery || harness.delivery.held() {
		t.Fatalf("Always delivery lease: held_at_insert=%t held_during_effect=%t held_after_return=%t",
			harness.mutations.createdWithDelivery, harness.executedWithDelivery, harness.delivery.held())
	}
	actions, observed := harness.mutations.snapshot()
	if fmt.Sprint(actions) != "[mcp.vault_action.request.created mcp.vault_action.completed]" || len(observed) != 0 {
		t.Fatalf("actions=%v observed=%v", actions, observed)
	}
}

func TestRuntimeGetOwnedConcealsTokensAndStalesDriftedApproval(t *testing.T) {
	harness := newRuntimeHarness(t)
	created := harness.call(t, "owned-request")
	if err := harness.runtime.DeliverOwned(t.Context(), created.ID, harness.otherTokenID, func(RequestView) {}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other token error = %v", err)
	}
	harness.authorization = OutputContextStale
	var callView RequestView
	if err := harness.runtime.DeliverCallResult(t.Context(), created.ID, harness.tokenID, func(delivered RequestView) {
		callView = delivered
	}); err != nil || callView.OutputAuthorized || callView.Request.Status != StatusApprovalPending {
		t.Fatalf("idempotent call delivery = %#v err=%v", callView, err)
	}
	var view RequestView
	err := harness.runtime.DeliverOwned(t.Context(), created.ID, harness.tokenID, func(delivered RequestView) {
		if !harness.delivery.held() {
			t.Fatal("drifted output authorization ran without delivery admission")
		}
		view = delivered
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.OutputAuthorized || view.Request.Status != StatusStale {
		t.Fatalf("drifted request = %#v", view)
	}
}

func TestRuntimeGetOwnedPreservesPendingApprovalForTemporaryOutputWithholding(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*runtimeHarness)
	}{
		{name: "MCP stopped", apply: func(harness *runtimeHarness) { harness.mcpStarted = false }},
		{name: "authorization unavailable", apply: func(harness *runtimeHarness) { harness.authorization = OutputWithheld }},
		{name: "sealed request unavailable", apply: func(harness *runtimeHarness) { harness.openErr = errors.New("temporary decrypt failure") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			created := harness.call(t, "temporary-withholding-"+strings.ReplaceAll(test.name, " ", "-"))
			test.apply(harness)
			var view RequestView
			if err := harness.runtime.DeliverOwned(t.Context(), created.ID, harness.tokenID, func(delivered RequestView) {
				view = delivered
			}); err != nil {
				t.Fatal(err)
			}
			if view.OutputAuthorized || view.Request.Status != StatusApprovalPending {
				t.Fatalf("temporary withholding consumed pending approval: %#v", view)
			}
		})
	}
}

func TestRuntimeApprovalAndCancellationTransitions(t *testing.T) {
	t.Run("run", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		created := harness.call(t, "run-request")
		result, err := harness.runtime.RunPending(t.Context(), created.ID, "approved")
		if err != nil || result.Request.Status != StatusCompleted || harness.executeCalls != 1 {
			t.Fatalf("result=%#v execute=%d err=%v", result, harness.executeCalls, err)
		}
	})
	t.Run("stopped", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		created := harness.call(t, "stopped-request")
		harness.mcpStarted = false
		if _, err := harness.runtime.RunPending(t.Context(), created.ID, ""); !errors.Is(err, ErrMCPExecutionStopped) {
			t.Fatalf("RunPending() error = %v", err)
		}
	})
	t.Run("decline", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		created := harness.call(t, "decline-request")
		item, err := harness.runtime.DeclinePending(t.Context(), created.ID, "not now")
		if err != nil || item.Status != StatusDeclined || item.UserNote != "not now" {
			t.Fatalf("item=%#v err=%v", item, err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		created := harness.call(t, "cancel-request")
		if _, err := harness.runtime.CancelOwned(t.Context(), created.ID, harness.otherTokenID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("other token cancel error = %v", err)
		}
		item, err := harness.runtime.CancelOwned(t.Context(), created.ID, harness.tokenID)
		if err != nil || item.Status != StatusCanceled {
			t.Fatalf("item=%#v err=%v", item, err)
		}
	})
}

func TestRuntimeRedactsOperatorNoteBeforeEveryDecisionPath(t *testing.T) {
	for _, test := range []struct {
		name     string
		decision func(*runtimeHarness, int64) (Request, error)
		status   string
	}{
		{
			name: "run success", status: StatusCompleted,
			decision: func(harness *runtimeHarness, id int64) (Request, error) {
				result, err := harness.runtime.RunPending(t.Context(), id, "keep CANARY password=raw-secret")
				return result.Request, err
			},
		},
		{
			name: "run failure", status: StatusFailed,
			decision: func(harness *runtimeHarness, id int64) (Request, error) {
				harness.runtime.execute = func(context.Context, Request) (any, error) {
					return nil, errors.New("synthetic execution failure")
				}
				result, err := harness.runtime.RunPending(t.Context(), id, "keep CANARY password=raw-secret")
				return result.Request, err
			},
		},
		{
			name: "decline", status: StatusDeclined,
			decision: func(harness *runtimeHarness, id int64) (Request, error) {
				return harness.runtime.DeclinePending(t.Context(), id, "keep CANARY password=raw-secret")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			harness.runtime.redactProjection = func(_ context.Context, value any) (any, error) {
				text, ok := value.(string)
				if !ok {
					return value, nil
				}
				text = strings.ReplaceAll(text, "CANARY", "[REDACTED]")
				text = strings.ReplaceAll(text, "raw-secret", "[REDACTED]")
				return text, nil
			}
			created := harness.call(t, "redact-"+strings.ReplaceAll(test.name, " ", "-"))
			item, err := test.decision(harness, created.ID)
			if err != nil || item.Status != test.status {
				t.Fatalf("decision item=%#v err=%v", item, err)
			}
			persisted, err := harness.store.Get(t.Context(), created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.UserNote != "keep [REDACTED] password=[REDACTED]" {
				t.Fatalf("persisted user note = %q", persisted.UserNote)
			}
			var historyNote string
			if err := harness.database.QueryRow(`
				SELECT user_note FROM history_entries
				WHERE source_ref_type = 'vault_action_request' AND source_ref_id = ?`, created.ID).Scan(&historyNote); err != nil {
				t.Fatal(err)
			}
			if historyNote != persisted.UserNote {
				t.Fatalf("history note = %q, want %q", historyNote, persisted.UserNote)
			}
		})
	}

	t.Run("atomic executor", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		created := harness.call(t, "redact-atomic")
		harness.runtime.redactProjection = func(_ context.Context, value any) (any, error) {
			text, ok := value.(string)
			if !ok {
				return value, nil
			}
			return strings.ReplaceAll(text, "CANARY", "[REDACTED]"), nil
		}
		var atomicNote string
		harness.runtime.executeAtomic = func(ctx context.Context, request Request, _ string, userNote string, _ string) (WorkflowResult, bool, error) {
			atomicNote = userNote
			completed, err := harness.store.Complete(ctx, request.ID, StatusFailed, nil, "synthetic atomic failure", userNote)
			return WorkflowResult{Request: completed, ExecutionError: errors.New("synthetic atomic failure")}, true, err
		}
		result, err := harness.runtime.RunPending(t.Context(), created.ID, "keep CANARY")
		if err != nil || result.Request.Status != StatusFailed || atomicNote != "keep [REDACTED]" || result.Request.UserNote != atomicNote {
			t.Fatalf("atomic result=%#v note=%q err=%v", result, atomicNote, err)
		}
	})

	t.Run("redaction failure does not claim", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		created := harness.call(t, "redact-failure")
		harness.runtime.redactProjection = func(context.Context, any) (any, error) {
			return nil, errors.New("redaction unavailable")
		}
		if _, err := harness.runtime.RunPending(t.Context(), created.ID, "CANARY"); err == nil || !strings.Contains(err.Error(), "redaction unavailable") {
			t.Fatalf("redaction error = %v", err)
		}
		persisted, err := harness.store.Get(t.Context(), created.ID)
		if err != nil || persisted.Status != StatusApprovalPending || persisted.UserNote != "" {
			t.Fatalf("request changed after redaction failure: %#v err=%v", persisted, err)
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
