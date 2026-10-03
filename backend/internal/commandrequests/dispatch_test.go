package commandrequests

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

func TestDispatchAdmissionIsDurableAndOneWinner(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	store := NewStore(database)
	projection := &testCommandProjection{}
	id, err := store.Insert(t.Context(), testCommandCodec{}, projection, PreparedInsert{
		insert: Insert{RuntimeID: runtimeID, Command: "effect", Status: "running", Queued: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	failedProjection := errors.New("projection failed")
	if err := store.ClaimDispatch(t.Context(), &testCommandProjection{err: failedProjection}, id); !errors.Is(err, failedProjection) {
		t.Fatalf("claim error: %v", err)
	}
	var state string
	if err := database.QueryRowContext(t.Context(), "SELECT dispatch_state FROM command_requests WHERE id = ?", id).Scan(&state); err != nil || state != "queued" {
		t.Fatalf("failed claim committed state %q: %v", state, err)
	}
	if err := store.ClaimDispatch(t.Context(), projection, id); err != nil {
		t.Fatal(err)
	}
	if err := store.ClaimDispatch(t.Context(), projection, id); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("repeated claim authorized dispatch: %v", err)
	}
	if err := store.CancelRunning(t.Context(), projection, "closed after admission"); err != nil {
		t.Fatal(err)
	}
	if err := store.ClaimDispatch(t.Context(), projection, id); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("terminal claim authorized dispatch: %v", err)
	}
	if err := store.SetSession(t.Context(), projection, id, 99); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("terminal result accepted a late session binding: %v", err)
	}
	if err := database.QueryRowContext(t.Context(), "SELECT dispatch_state FROM command_requests WHERE id = ?", id).Scan(&state); err != nil || state != "admitted" {
		t.Fatalf("admission evidence lost %q: %v", state, err)
	}
}

type callbackBulkSessions func(context.Context, executionprincipal.Principal, int64, string) (console.ExecResult, error)

func (call callbackBulkSessions) Exec(ctx context.Context, principal executionprincipal.Principal, id int64, command string) (console.ExecResult, error) {
	return call(ctx, principal, id, command)
}

func TestBulkDispatchBoundaryAndLateReplies(t *testing.T) {
	for _, variant := range []string{"claim-failed", "terminal-winner", "known-success", "known-failure", "lost-reply", "canceled-before-admission"} {
		t.Run(variant, func(t *testing.T) {
			runtime, owner, _ := newBulkTestRuntime(t)
			worker, cancel := context.WithCancel(t.Context())
			defer cancel()
			sends := 0
			runtime.Sessions = callbackBulkSessions(func(context.Context, executionprincipal.Principal, int64, string) (console.ExecResult, error) {
				sends++
				cancel()
				result := console.ExecResult{SessionID: 44, ExitCode: 0, Output: "known output"}
				if variant == "known-failure" {
					result.ExitCode = 17
				}
				if variant == "lost-reply" {
					return result, context.Canceled
				}
				return result, nil
			})
			expected := "completed"
			switch variant {
			case "claim-failed":
				owner.claimError, expected = errors.New("database unavailable"), "error"
			case "terminal-winner":
				owner.claimError = ErrNotRunning
			case "known-failure":
				expected = "failed"
			case "lost-reply":
				expected = "outcome_unknown"
			case "canceled-before-admission":
				cancel()
			}
			runtime.runOne(worker, BulkHTTPResponseItem{RequestID: 7, TargetID: 5}, "effect")
			if variant == "terminal-winner" || variant == "canceled-before-admission" {
				if sends != 0 || len(owner.finish) != 0 {
					t.Fatalf("unowned request dispatched/finished: sends=%d completions=%d", sends, len(owner.finish))
				}
				return
			}
			select {
			case completion := <-owner.finish:
				if completion.Status != expected {
					t.Fatalf("completion=%#v want=%s", completion, expected)
				}
				if variant != "claim-failed" && (sends != 1 || completion.SessionID != 44) {
					t.Fatalf("lost execution identity: sends=%d completion=%#v", sends, completion)
				}
				if variant == "known-success" && completion.Stdout != "known output" {
					t.Fatal("shutdown discarded known output")
				}
				if variant == "known-failure" && completion.ExitCode != 17 {
					t.Fatal("shutdown discarded known failure")
				}
				if variant == "claim-failed" && sends != 0 {
					t.Fatal("dispatch after failed admission")
				}
			default:
				t.Fatal("known completion was discarded during shutdown")
			}
		})
	}
}
