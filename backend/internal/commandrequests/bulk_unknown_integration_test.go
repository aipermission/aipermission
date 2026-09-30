package commandrequests

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestBulkUnknownPersistsHistoryAndHTTPReplayIdentity(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	secretVault, err := vault.New("bulk-unknown-fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewWorkspaceRuntime(WorkspaceRuntimeDependencies{
		Database: database, Vault: secretVault, WorkspaceID: "bulk-unknown-test",
		Redact: securitypolicy.NewService(database).Redact, Sessions: &testActiveSessions{},
		BackgroundTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.StopWorkers(context.Background()); err != nil {
			t.Error(err)
		}
	})
	principal, err := executionprincipal.LocalOperator("workspace", "runtime-instance")
	if err != nil {
		t.Fatal(err)
	}
	sessions := &fakeBulkSessions{
		result: console.ExecResult{SessionID: 44, Generation: 3, Running: true, Output: "withheld-output-fixture"},
		err:    fmt.Errorf("observe: %w", console.ErrCommandOutcomeUnknown), calls: make(chan int64, 2),
	}
	runtime := &BulkHTTPRuntime{
		Requests: owner, Sessions: sessions, Principal: func() (executionprincipal.Principal, error) { return principal, nil },
		ResolveTarget: func(_ context.Context, id int64) (BulkTarget, error) {
			return BulkTarget{RuntimeID: id, Name: "target"}, nil
		},
		WithTransaction: func(ctx context.Context, run func(*sql.Tx, BulkAuditAppender) error) error {
			tx, err := database.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if err := run(tx, func(*sql.Tx, string, *int64, int64, string, any) error { return nil }); err != nil {
				return err
			}
			return tx.Commit()
		},
	}
	handler := NewBulkHTTPHandlers(func(http.ResponseWriter) (*BulkHTTPRuntime, bool) { return runtime, true })
	body := fmt.Sprintf(`{"target_ids":[%d],"command":"echo test","confirmation":"RUN ON 1 TARGETS","idempotency_key":"persisted-unknown-fixture"}`, runtimeID)
	first := httptest.NewRecorder()
	handler.Run(first, bulkRequest(body))
	var response BulkHTTPResponse
	if first.Code != http.StatusAccepted || json.Unmarshal(first.Body.Bytes(), &response) != nil || len(response.Items) != 1 {
		t.Fatalf("bulk reply: %d %s", first.Code, first.Body.String())
	}
	id := response.Items[0].RequestID
	deadline := time.Now().Add(3 * time.Second)
	for {
		record, err := owner.Get(t.Context(), id, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		if record.Status == "outcome_unknown" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker did not finish: %#v", record)
		}
		time.Sleep(10 * time.Millisecond)
	}
	replay := httptest.NewRecorder()
	handler.Run(replay, bulkRequest(body))
	if replay.Code != http.StatusAccepted || replay.Body.String() != first.Body.String() || len(sessions.calls) != 1 {
		t.Fatalf("durable replay redispatched: status=%d calls=%d body=%s", replay.Code, len(sessions.calls), replay.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/api/console/command-requests/"+strconv.FormatInt(id, 10), nil)
	request.SetPathValue("id", strconv.FormatInt(id, 10))
	detail := httptest.NewRecorder()
	NewHTTPHandlers(func(http.ResponseWriter) (HTTPReader, bool) { return owner, true }).Get(detail, request)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"status":"outcome_unknown"`) || !strings.Contains(detail.Body.String(), `"session_id":44`) || strings.Contains(detail.Body.String(), "withheld-output-fixture") {
		t.Fatalf("HTTP lost safe outcome: %d %s", detail.Code, detail.Body.String())
	}
	var status, output string
	if err := database.QueryRowContext(t.Context(), `SELECT status, output_text FROM history_entries WHERE source_ref_type = 'command_request' AND source_ref_id = ?`, id).Scan(&status, &output); err != nil {
		t.Fatal(err)
	}
	if status != "outcome_unknown" || output != "" {
		t.Fatalf("history lost safe outcome: status=%q output=%q", status, output)
	}
}
