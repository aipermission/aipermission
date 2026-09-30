package commandrequests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestBulkUnknownOutcomeRetainsHandleAndReplaysWithoutDispatch(t *testing.T) {
	for _, unknown := range []bool{true, false} {
		name := "before-dispatch"
		if unknown {
			name = "after-dispatch"
		}
		t.Run(name, func(t *testing.T) {
			runtime, owner, sessions := newBulkTestRuntime(t)
			sessions.err = errors.New("connection refused")
			if unknown {
				sessions.err = fmt.Errorf("observe: %w", console.ErrCommandOutcomeUnknown)
				sessions.result = console.ExecResult{SessionID: 44, Generation: 3, Running: true, Output: "withheld-output-fixture"}
			}
			handler := NewBulkHTTPHandlers(func(http.ResponseWriter) (*BulkHTTPRuntime, bool) { return runtime, true })
			body := `{"target_ids":[5],"command":"echo test","confirmation":"RUN ON 1 TARGETS","idempotency_key":"unknown-replay-fixture"}`
			response := httptest.NewRecorder()
			handler.Run(response, bulkRequest(body))
			if response.Code != http.StatusAccepted {
				t.Fatalf("run status=%d body=%s", response.Code, response.Body.String())
			}
			select {
			case completion := <-owner.finish:
				status, sessionID := "error", int64(0)
				if unknown {
					status, sessionID = "outcome_unknown", 44
				}
				if completion.Status != status || completion.SessionID != sessionID || completion.Stdout != "" || completion.Stderr != "" {
					t.Fatalf("completion lost dispatch classification: %#v", completion)
				}
			case <-time.After(time.Second):
				t.Fatal("command did not complete")
			}
			<-sessions.calls
			replay := httptest.NewRecorder()
			handler.Run(replay, bulkRequest(body))
			if replay.Code != http.StatusAccepted || replay.Body.String() != response.Body.String() {
				t.Fatalf("replay changed request identity: %d %s", replay.Code, replay.Body.String())
			}
			select {
			case runtimeID := <-sessions.calls:
				t.Fatalf("same-key replay dispatched again to %d", runtimeID)
			case <-time.After(25 * time.Millisecond):
			}
			if len(owner.finishedActive) != 0 {
				t.Fatal("unknown result started an unauthorized observation worker")
			}
		})
	}
}

func TestBackgroundObservationFailureRemainsUnknownInStorage(t *testing.T) {
	for _, observeErr := range []error{
		context.DeadlineExceeded,
		context.Canceled,
		fmt.Errorf("observe: %w", console.ErrCommandOutcomeUnknown),
		errors.New("console transport disconnected"),
	} {
		t.Run(observeErr.Error(), func(t *testing.T) {
			database, runtimeID := commandRequestFixture(t)
			sessions := &testActiveSessions{err: observeErr, result: console.ExecResult{Output: "withheld-output-fixture"}}
			secretVault, err := vault.New("unknown-command-test-key")
			if err != nil {
				t.Fatal(err)
			}
			owner, err := NewWorkspaceRuntime(WorkspaceRuntimeDependencies{
				Database: database, Vault: secretVault, WorkspaceID: "unknown-command-test",
				Redact:   securitypolicy.NewService(database).Redact,
				Sessions: sessions, BackgroundTimeout: time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			id, err := owner.Insert(t.Context(), Insert{RuntimeID: runtimeID, Command: "remote effect", Status: "running"})
			if err != nil {
				t.Fatal(err)
			}
			principal, err := executionprincipal.LocalOperator("workspace", "runtime-instance")
			if err != nil {
				t.Fatal(err)
			}
			owner.FinishActive(t.Context(), id, principal, console.SessionHandle{ID: 44, RuntimeID: runtimeID, Generation: 3})
			record, err := owner.Get(t.Context(), id, 0, "")
			if err != nil || record.Status != "outcome_unknown" || record.SessionID == nil || *record.SessionID != 44 || record.Stdout != "" || record.Stderr != "" {
				t.Fatalf("stored dispatch uncertainty = %#v, %v", record, err)
			}
			if !strings.Contains(record.Error, "inspect") || !strings.Contains(record.Error, "retry") || record.AssistantHint != commandObservationUnknown || record.RetryAfterSeconds != 0 {
				t.Fatalf("missing reconciliation guidance: %q", record.Error)
			}
			var historyStatus, historyOutput, historyError string
			if err := database.QueryRowContext(t.Context(), `SELECT status, output_text, error FROM history_entries WHERE source_ref_type = 'command_request' AND source_ref_id = ?`, id).Scan(&historyStatus, &historyOutput, &historyError); err != nil {
				t.Fatal(err)
			}
			if historyStatus != "outcome_unknown" || historyOutput != "" || historyError != record.Error {
				t.Fatalf("history lost uncertainty: status=%q output=%q error=%q", historyStatus, historyOutput, historyError)
			}
		})
	}
}
